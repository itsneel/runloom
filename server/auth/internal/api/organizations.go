package api

import (
	"fmt"
	"github.com/example/runloom/auth/internal/access"
	"net/http"
)

func (g *Gateway) organizationCreation(w http.ResponseWriter, r *http.Request) {
	s, _, err := g.Directory.Repo.Read(r.Context())
	if err != nil {
		writeError(w, 503, fmt.Errorf("organization directory unavailable"))
		return
	}
	u := principal(r)
	parents := []access.OrganizationSummary{}
	limit := access.ChildOrganizationLimit(s)
	if id := r.URL.Query().Get("organization_id"); id != "" {
		if access.OrgRole(s, id, u.ID) == "" {
			accessError(w, access.ErrForbidden)
			return
		}
		root := s.Orgs[id]
		if root.ParentID != "" {
			root = s.Orgs[root.ParentID]
		}
		limit = access.OrganizationChildLimit(s, root.ID)
		if root.ID != "" && root.ParentID == "" && access.IsOrgAdmin(access.OrgRole(s, root.ID, u.ID)) {
			parents = append(parents, access.OrganizationSummary{ID: root.ID, Name: root.Name})
		}
	}
	writeJSON(w, 200, map[string]any{"can_create": access.CanCreateRootOrganization(s, u) || len(parents) > 0, "can_create_root": access.CanCreateRootOrganization(s, u), "max_children": limit, "parents": parents})
}
func (g *Gateway) organizationHierarchy(w http.ResponseWriter, r *http.Request) {
	s, _, err := g.Directory.Repo.Read(r.Context())
	if err != nil {
		writeError(w, 503, fmt.Errorf("organization directory unavailable"))
		return
	}
	hierarchy, err := access.Hierarchy(s, principal(r).ID, r.PathValue("org"))
	if err != nil {
		accessError(w, err)
		return
	}
	writeJSON(w, 200, hierarchy)
}
func (g *Gateway) renameOrganization(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, 400, err)
		return
	}
	if err := g.Directory.RenameOrganization(r.Context(), principal(r), r.PathValue("org"), body.Name); err != nil {
		accessError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"saved": true})
}
func (g *Gateway) organizationSettings(w http.ResponseWriter, r *http.Request) {
	s, ok := g.platformState(w, r, "platform.organizations.configure")
	if !ok {
		return
	}
	if r.Method == "GET" {
		writeJSON(w, 200, map[string]int{"max_children": access.ChildOrganizationLimit(s), "maximum": 10})
		return
	}
	var body struct {
		MaxChildren *int `json:"max_children"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, 400, err)
		return
	}
	if body.MaxChildren == nil {
		writeError(w, 400, fmt.Errorf("max_children is required"))
		return
	}
	if err := g.Directory.SetChildOrganizationLimit(r.Context(), principal(r), *body.MaxChildren); err != nil {
		accessError(w, err)
		return
	}
	writeJSON(w, 200, map[string]int{"max_children": *body.MaxChildren, "maximum": 10})
}
