package api

import (
	"fmt"
	"net/http"

	"github.com/example/runloom/auth/internal/access"
)

func (g *Gateway) organizationChildLimit(w http.ResponseWriter, r *http.Request) {
	permission := "platform.organizations.configure"
	if r.Method == "GET" {
		permission = "platform.organizations.read"
	}
	s, ok := g.platformState(w, r, permission)
	if !ok {
		return
	}
	id := r.PathValue("org")
	policy, err := access.ChildOrganizationPolicy(s, id)
	if err != nil {
		accessError(w, err)
		return
	}
	if r.Method == "GET" {
		writeJSON(w, 200, policy)
		return
	}
	var limit *int
	if r.Method == "PUT" {
		var body struct {
			MaxChildren *int `json:"max_children"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			writeError(w, 400, err)
			return
		}
		if body.MaxChildren == nil {
			writeError(w, 400, fmt.Errorf("max_children is required; use DELETE to restore the platform default"))
			return
		}
		limit = body.MaxChildren
	}
	if err := g.Directory.SetOrganizationChildLimit(r.Context(), principal(r), id, limit); err != nil {
		accessError(w, err)
		return
	}
	// Return the committed policy rather than predicting a concurrently changed default.
	s, ok = g.platformState(w, r, permission)
	if !ok {
		return
	}
	policy, err = access.ChildOrganizationPolicy(s, id)
	if err != nil {
		accessError(w, err)
		return
	}
	writeJSON(w, 200, policy)
}
