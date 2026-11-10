package api

import (
	"fmt"
	"github.com/example/runloom/workflows/internal/access"
	"github.com/example/runloom/workflows/internal/dbnode"
	"net/http"
	"sort"
	"time"
)

func (g *Gateway) connections(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	u := principal(r)
	org, team, id := r.PathValue("org"), r.PathValue("team"), r.PathValue("connection")
	s, _, err := g.Directory.Repo.Read(r.Context())
	if err != nil {
		accessError(w, err)
		return
	}
	actual, role := access.TeamRole(s, team, u.ID)
	if actual != org || role == "" {
		accessError(w, access.ErrForbidden)
		return
	}
	if (r.Method == "PUT" || r.Method == "DELETE") && role != "admin" {
		accessError(w, access.ErrForbidden)
		return
	}
	if r.Method == "GET" && id == "" {
		items := []dbnode.Connection{}
		for _, item := range s.Connections {
			if item.TeamID == team {
				items = append(items, item.Connection.Public())
			}
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
		writeJSON(w, 200, map[string]any{"configured": g.Secrets != nil, "items": items})
		return
	}
	if g.Secrets == nil {
		writeError(w, 503, fmt.Errorf("connection vault is not configured"))
		return
	}
	switch r.Method {
	case "PUT":
		var c dbnode.Connection
		if decodeJSON(w, r, &c) != nil {
			writeError(w, 400, fmt.Errorf("invalid connection settings"))
			return
		}
		c.ID = id
		if err := dbnode.Configured().Network.Check(c.Host, c.Port, c.TLS); err != nil {
			writeError(w, 400, err)
			return
		}
		err = g.Secrets.PutConnection(r.Context(), u, org, team, c)
	case "DELETE":
		err = g.Directory.UpdateModule(r.Context(), "secrets", func(s *access.State) error {
			actual, role := access.TeamRole(*s, team, u.ID)
			if actual != org || role != "admin" {
				return access.ErrForbidden
			}
			key := team + "/" + id
			if _, ok := s.Connections[key]; !ok {
				return access.ErrNotFound
			}
			delete(s.Connections, key)
			s.Events = append(s.Events, access.Event{OrgID: org, Actor: u.ID, Action: "connection.deleted", Target: key, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
			return nil
		})
	default:
		if role != "admin" && role != "editor" {
			accessError(w, access.ErrForbidden)
			return
		}
		var c dbnode.Connection
		c, err = g.Secrets.ResolveConnection(r.Context(), team, id)
		if err != nil {
			writeError(w, 404, fmt.Errorf("connection is unavailable"))
			return
		}
		if r.PathValue("action") == "test" && r.Method == "POST" {
			err = dbnode.Configured().Test(r.Context(), c)
		} else if r.Method == "GET" {
			var tables []dbnode.Table
			tables, err = dbnode.Configured().Discover(r.Context(), c)
			if err == nil {
				writeJSON(w, 200, map[string]any{"tables": tables})
				return
			}
		} else {
			http.NotFound(w, r)
			return
		}
	}
	if err != nil {
		if r.Method == "PUT" || r.Method == "DELETE" {
			accessError(w, err)
		} else {
			writeError(w, 400, err)
		}
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
