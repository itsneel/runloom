package api

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/example/runloom/workflows/internal/access"
)

func (g *Gateway) secrets(w http.ResponseWriter, r *http.Request) {
	u := principal(r)
	org, team := r.PathValue("org"), r.PathValue("team")
	s, _, err := g.Directory.Repo.Read(r.Context())
	if err != nil {
		accessError(w, err)
		return
	}
	actualOrg, role := access.TeamRole(s, team, u.ID)
	if org != actualOrg || role == "" || (r.Method != "GET" && role != "admin") {
		accessError(w, access.ErrForbidden)
		return
	}
	if r.Method == "GET" {
		items := []map[string]string{}
		for _, secret := range s.Secrets {
			if secret.TeamID == team {
				items = append(items, map[string]string{"name": secret.Name, "updated_at": secret.UpdatedAt})
			}
		}
		sort.Slice(items, func(i, j int) bool { return items[i]["name"] < items[j]["name"] })
		writeJSON(w, 200, map[string]any{"configured": g.Secrets != nil, "items": items})
		return
	}
	if r.Method == "DELETE" {
		err = g.Directory.UpdateModule(r.Context(), "secrets", func(s *access.State) error {
			actualOrg, role := access.TeamRole(*s, team, u.ID)
			if actualOrg != org || role != "admin" {
				return access.ErrForbidden
			}
			key := team + "/" + r.PathValue("name")
			if _, exists := s.Secrets[key]; !exists {
				return access.ErrNotFound
			}
			delete(s.Secrets, key)
			s.Events = append(s.Events, access.Event{OrgID: org, Actor: u.ID, Action: "secret.deleted", Target: key, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
			return nil
		})
	} else {
		if g.Secrets == nil {
			writeError(w, 503, fmt.Errorf("workflow secrets require RUNLOOM_SECRETS_KEY_FILE on API and worker"))
			return
		}
		var body struct {
			Value string `json:"value"`
		}
		if decodeJSON(w, r, &body) != nil {
			writeError(w, 400, fmt.Errorf("invalid secret request"))
			return
		}
		err = g.Secrets.Put(r.Context(), u, org, team, r.PathValue("name"), body.Value)
	}
	if err != nil {
		accessError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"saved": true})
}
