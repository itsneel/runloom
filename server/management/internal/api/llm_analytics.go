package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/example/runloom/management/internal/access"
	"github.com/example/runloom/management/internal/usage"
)

const analyticsRequestLimit = 50000

func (g *Gateway) llmAnalytics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	s, _, err := g.Directory.Repo.Read(ctx)
	if err != nil {
		writeError(w, 503, fmt.Errorf("account directory unavailable"))
		return
	}
	org := r.PathValue("org")
	user := principal(r)
	if access.OrgRole(s, org, user.ID) == "" {
		accessError(w, access.ErrForbidden)
		return
	}
	q := r.URL.Query()
	period := q.Get("period")
	if period == "" {
		period = "1m"
	}
	now := time.Now().UTC()
	asOf := now
	if q.Get("as_of") != "" {
		asOf, err = time.Parse(time.RFC3339, q.Get("as_of"))
		if err != nil || asOf.After(now) || now.Sub(asOf) > 24*time.Hour {
			writeError(w, 400, fmt.Errorf("as_of must be a UTC timestamp within the past 24 hours; refresh the dashboard"))
			return
		}
	}
	window, err := usage.AnalyticsWindow(period, asOf)
	if err != nil {
		writeError(w, 400, err)
		return
	}
	mode := q.Get("mode")
	if mode == "" {
		mode = "all"
	}
	if mode != "all" && mode != "test" && mode != "production" {
		writeError(w, 400, fmt.Errorf("invalid mode"))
		return
	}
	field := q.Get("sort")
	if field == "" {
		field = "cost"
	}
	if field != "cost" && field != "name" && field != "tokens" && field != "requests" && field != "runs" && field != "latest" {
		writeError(w, 400, fmt.Errorf("invalid sort"))
		return
	}
	direction := q.Get("direction")
	if direction == "" {
		direction = "desc"
	}
	if direction != "asc" && direction != "desc" {
		writeError(w, 400, fmt.Errorf("invalid direction"))
		return
	}
	group := q.Get("group")
	if group == "" {
		group = "runs"
	}
	if group != "runs" && group != "teams" && group != "models" {
		writeError(w, 400, fmt.Errorf("invalid group"))
		return
	}
	page := 0
	if q.Get("page") != "" {
		page, err = strconv.Atoi(q.Get("page"))
		if err != nil || page < 0 || page > analyticsRequestLimit/50 {
			writeError(w, 400, fmt.Errorf("invalid page"))
			return
		}
	}
	allowed := []access.TeamView{}
	for id, t := range s.Orgs[org].Teams {
		_, role := access.TeamRole(s, id, user.ID)
		if role != "" {
			allowed = append(allowed, access.TeamView{ID: id, Name: t.Name, Role: role})
		}
	}
	sort.Slice(allowed, func(i, j int) bool { return allowed[i].ID < allowed[j].ID })
	selected := q.Get("team")
	names := map[string]string{}
	for _, team := range allowed {
		if selected == "" || selected == team.ID {
			names[team.ID] = team.Name
		}
	}
	if selected != "" && len(names) == 0 {
		accessError(w, access.ErrForbidden)
		return
	}
	if len(names) > 100 {
		writeError(w, 422, fmt.Errorf("select a team; analytics supports at most 100 teams per request"))
		return
	}
	entries := []usage.Entry{}
	scanned := 0
	for _, team := range allowed {
		if _, ok := names[team.ID]; !ok {
			continue
		}
		tenant, err := g.tenant(ctx, team.ID)
		if err != nil {
			writeError(w, 503, fmt.Errorf("cost analytics unavailable; retry"))
			return
		}
		filter := usage.Filter{All: true, StartedFrom: window.PreviousFrom.Format("2006-01-02T15:04:05"), StartedBefore: window.Until.Format("2006-01-02T15:04:05"), Limit: 1000}
		for {
			batch, err := tenant.backend.QueryUsage(ctx, filter)
			if err != nil {
				writeError(w, 503, fmt.Errorf("cost analytics unavailable; no partial totals returned"))
				return
			}
			scanned += len(batch)
			if scanned > analyticsRequestLimit {
				writeError(w, 422, fmt.Errorf("too many recorded requests; choose a shorter period or one team (50,000 request limit including comparison period)"))
				return
			}
			for _, entry := range batch {
				// The authorized store, not a payload-supplied team identifier, owns the entry.
				entry.TeamID = team.ID
				if mode == "test" && !entry.IsTest || mode == "production" && entry.IsTest {
					continue
				}
				entries = append(entries, entry)
			}
			if len(batch) < filter.Limit {
				break
			}
			filter.After = batch[len(batch)-1].ID
		}
	}
	result := usage.Aggregate(entries, names, window, now)
	rows := result.Runs
	if group == "teams" {
		rows = result.Teams
	} else if group == "models" {
		rows = result.Models
	}
	usage.SortAnalytics(rows, field, direction)
	count := len(rows)
	start := page * 50
	if start > count {
		start = count
	}
	end := start + 50
	if end > count {
		end = count
	}
	// Recheck authorization after reading multiple stores, including revocation
	// during the request. Never return a partially authorized aggregate.
	current, _, err := g.Directory.Repo.Read(ctx)
	if err != nil {
		writeError(w, 503, fmt.Errorf("account directory unavailable"))
		return
	}
	if access.OrgRole(current, org, user.ID) == "" {
		accessError(w, access.ErrForbidden)
		return
	}
	for id := range names {
		oid, role := access.TeamRole(current, id, user.ID)
		if oid != org || role == "" {
			accessError(w, access.ErrForbidden)
			return
		}
	}
	for _, team := range allowed {
		oid, role := access.TeamRole(current, team.ID, user.ID)
		if oid != org || role == "" {
			accessError(w, access.ErrForbidden)
			return
		}
	}
	if err := ctx.Err(); err != nil {
		writeError(w, 503, fmt.Errorf("cost analytics timed out; choose a shorter period or one team"))
		return
	}
	writeJSON(w, 200, map[string]any{"window": result.Window, "currency": "USD", "timezone": "UTC", "summary": result.Summary, "previous": result.Previous, "trend": result.Trend, "rows": rows[start:end], "total_rows": count, "page": page, "page_size": 50, "group": group, "sort": field, "direction": direction, "allowed_teams": allowed, "scope": "authorized_teams_direct_requests", "recorded_requests_only": true, "as_of": window.Until})
}
