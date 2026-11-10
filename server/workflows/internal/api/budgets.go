package api

import (
	"context"
	"errors"
	"fmt"
	"github.com/example/runloom/workflows/internal/access"
	"github.com/example/runloom/workflows/internal/budget"
	"github.com/example/runloom/workflows/internal/store"
	"net/http"
	"sort"
	"strings"
	"time"
)

func (g *Gateway) budgetPolicies(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	now := time.Now().UTC()
	org, team, user := r.PathValue("org"), r.PathValue("team"), principal(r)
	authorized := func() bool {
		state, _, err := g.Directory.Repo.Read(ctx)
		if err != nil {
			writeError(w, 503, fmt.Errorf("directory unavailable"))
			return false
		}
		actual, role := access.TeamRole(state, team, user.ID)
		if actual != org || role == "" || (r.Method != "GET" && role != "admin") {
			accessError(w, access.ErrForbidden)
			return false
		}
		return true
	}
	if !authorized() {
		return
	}
	t, err := g.tenant(ctx, team)
	if err != nil {
		writeError(w, 503, fmt.Errorf("budget storage unavailable"))
		return
	}
	if r.Method == "PUT" {
		var p budget.Policy
		if err = decodeJSON(w, r, &p); err != nil {
			writeError(w, 400, err)
			return
		}
		if p.ID != "" && p.ID != r.PathValue("policy") {
			writeError(w, 400, fmt.Errorf("policy ID mismatch"))
			return
		}
		p.ID = r.PathValue("policy")
		if !authorized() {
			return
		}
		p, err = t.backend.SaveBudgetPolicy(ctx, p, user.ID)
		if err != nil {
			code := 400
			if errors.Is(err, store.ErrBudgetConflict) {
				code = 409
			} else if !errors.Is(err, store.ErrBudgetInput) && !errors.Is(err, store.ErrProjectNotFound) {
				code = 503
				err = fmt.Errorf("budget policy storage unavailable; refresh before retrying")
			}
			writeError(w, code, err)
			return
		}
		writeJSON(w, 200, p)
		return
	}
	c, err := t.backend.ProjectCatalog(ctx)
	if err != nil {
		writeError(w, 503, fmt.Errorf("budget policies unavailable"))
		return
	}
	totals, err := store.MonthlyBudgetSpend(ctx, t.backend, c, now)
	if err != nil {
		writeError(w, 503, fmt.Errorf("monthly budget accounting unavailable; no partial totals returned"))
		return
	}
	definitions, err := t.backend.List()
	if err != nil {
		writeError(w, 503, fmt.Errorf("workflow list unavailable"))
		return
	}
	policies := []budget.Policy{}
	projects := []store.Project{}
	spend := []store.BudgetSpend{}
	alerts := []budget.Decision{}
	for _, p := range c.Projects {
		projects = append(projects, p)
		spend = append(spend, totals[p.ID])
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].Name < projects[j].Name })
	sort.Slice(spend, func(i, j int) bool { return spend[i].ProjectName < spend[j].ProjectName })
	for _, p := range c.BudgetPolicies {
		policies = append(policies, p)
		for _, project := range projects {
			if p.Applies(project.ID, "") {
				total := totals[project.ID]
				alerts = append(alerts, budget.Evaluate(p, project.ID, project.Name, now, total.SpendUSD, total.Incomplete)...)
			}
		}
	}
	sort.Slice(policies, func(i, j int) bool { return policies[i].Name < policies[j].Name })
	sort.Slice(alerts, func(i, j int) bool {
		return alerts[i].PolicyID+alerts[i].ProjectID+alerts[i].Reason < alerts[j].PolicyID+alerts[j].ProjectID+alerts[j].Reason
	})
	events, err := t.backend.ListAudit("", "", 100)
	if err != nil {
		writeError(w, 503, fmt.Errorf("budget audit unavailable"))
		return
	}
	budgetEvents := []store.AuditEvent{}
	for _, e := range events {
		if strings.HasPrefix(e.EventType, "llm_budget.") {
			budgetEvents = append(budgetEvents, e)
		}
	}
	changes := []budget.Change{}
	for i := len(c.BudgetChanges) - 1; i >= 0 && len(changes) < 100; i-- {
		changes = append(changes, c.BudgetChanges[i])
	}
	if !authorized() {
		return
	}
	start, end := budget.Month(now)
	writeJSON(w, 200, map[string]any{"items": policies, "projects": projects, "workflows": definitions, "spend": spend, "alerts": alerts, "events": budgetEvents, "changes": changes, "month": start.Format("2006-01"), "resets_at": end.Format(time.RFC3339), "currency": "USD", "timezone": "UTC"})
}
