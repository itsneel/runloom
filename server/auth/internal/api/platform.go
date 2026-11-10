package api

import (
	_ "embed"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/example/runloom/auth/internal/access"
)

//go:embed platform.openapi.json
var platformOpenAPI []byte

type ServiceStatus struct {
	Service   string `json:"service"`
	Status    string `json:"status"`
	Detail    string `json:"detail"`
	LatencyMS int64  `json:"latency_ms"`
}

func (g *Gateway) registerPlatformRoutes() {
	g.management.HandleFunc("GET /api/v1/platform/me", g.platformMe)
	g.management.HandleFunc("GET /api/v1/platform/organizations", g.platformOrganizations)
	g.management.HandleFunc("GET /api/v1/platform/staff", g.platformStaff)
	g.management.HandleFunc("PUT /api/v1/platform/staff", g.platformSetRole)
	g.management.HandleFunc("GET /api/v1/platform/audit", g.platformAudit)
	g.management.HandleFunc("GET /api/v1/platform/health", g.platformHealth)
	g.management.HandleFunc("GET /api/v1/platform/organization-settings", g.organizationSettings)
	g.management.HandleFunc("PUT /api/v1/platform/organization-settings", g.organizationSettings)
	g.management.HandleFunc("GET /api/v1/platform/organizations/{org}/child-limit", g.organizationChildLimit)
	g.management.HandleFunc("PUT /api/v1/platform/organizations/{org}/child-limit", g.organizationChildLimit)
	g.management.HandleFunc("DELETE /api/v1/platform/organizations/{org}/child-limit", g.organizationChildLimit)
}

func (g *Gateway) platformState(w http.ResponseWriter, r *http.Request, permission string) (access.State, bool) {
	// No role cache: revocation takes effect on the next request.
	s, _, err := g.Directory.Repo.Module("identity").ReadOwned(r.Context())
	if err != nil {
		writeError(w, 503, fmt.Errorf("directory unavailable"))
		return s, false
	}
	if permission != "" && !access.HasPlatformPermission(s, principal(r).ID, permission) {
		accessError(w, access.ErrForbidden)
		return s, false
	}
	return s, true
}

func (g *Gateway) platformMe(w http.ResponseWriter, r *http.Request) {
	s, ok := g.platformState(w, r, "")
	if !ok {
		return
	}
	role := s.PlatformRoles[principal(r).ID]
	writeJSON(w, 200, map[string]any{"role": role, "permissions": access.PlatformPermissions(role)})
}

func platformPage[T any](w http.ResponseWriter, r *http.Request, items []T) {
	offset, limit := 0, 50
	var err error
	if v := r.URL.Query().Get("offset"); v != "" {
		offset, err = strconv.Atoi(v)
	}
	if err != nil || offset < 0 {
		writeError(w, 400, fmt.Errorf("invalid offset"))
		return
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, err = strconv.Atoi(v)
	}
	if err != nil || limit < 1 || limit > 100 {
		writeError(w, 400, fmt.Errorf("limit must be between 1 and 100"))
		return
	}
	total := len(items)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	writeJSON(w, 200, map[string]any{"items": items[offset:end], "total": total, "offset": offset, "limit": limit})
}

func (g *Gateway) platformOrganizations(w http.ResponseWriter, r *http.Request) {
	s, ok := g.platformState(w, r, "platform.organizations.read")
	if !ok {
		return
	}
	type metadata struct {
		ID         string                          `json:"id"`
		Name       string                          `json:"name"`
		Teams      int                             `json:"team_count"`
		Members    int                             `json:"member_count"`
		ParentID   string                          `json:"parent_id,omitempty"`
		Children   int                             `json:"child_count"`
		ChildLimit *access.OrganizationChildPolicy `json:"child_limit,omitempty"`
	}
	items := []metadata{}
	counts := map[string]int{}
	for _, org := range s.Orgs {
		if org.ParentID != "" {
			counts[org.ParentID]++
		}
	}
	for _, org := range s.Orgs {
		item := metadata{ID: org.ID, Name: org.Name, Teams: len(org.Teams), Members: len(org.Members), ParentID: org.ParentID, Children: counts[org.ID]}
		if org.ParentID == "" {
			policy, _ := access.ChildOrganizationPolicy(s, org.ID)
			item.ChildLimit = &policy
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	platformPage(w, r, items)
}

func (g *Gateway) platformStaff(w http.ResponseWriter, r *http.Request) {
	s, ok := g.platformState(w, r, "platform.staff.manage")
	if !ok {
		return
	}
	type member struct {
		access.User
		Role string `json:"role"`
	}
	items := []member{}
	for id, role := range s.PlatformRoles {
		items = append(items, member{s.Users[id], role})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	platformPage(w, r, items)
}

func (g *Gateway) platformSetRole(w http.ResponseWriter, r *http.Request) {
	if _, ok := g.platformState(w, r, "platform.staff.manage"); !ok {
		return
	}
	var input struct {
		Email  string `json:"email"`
		Role   string `json:"role"`
		Reason string `json:"reason"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, 400, err)
		return
	}
	if err := g.Directory.SetPlatformRole(r.Context(), principal(r), input.Email, input.Role, input.Reason); err != nil {
		accessError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"saved": true})
}

func (g *Gateway) platformAudit(w http.ResponseWriter, r *http.Request) {
	s, ok := g.platformState(w, r, "platform.audit.read")
	if !ok {
		return
	}
	items := []access.PlatformEvent{}
	for i := len(s.PlatformEvents) - 1; i >= 0; i-- {
		items = append(items, s.PlatformEvents[i])
	}
	platformPage(w, r, items)
}

func (g *Gateway) platformHealth(w http.ResponseWriter, r *http.Request) {
	if _, ok := g.platformState(w, r, "platform.health.read"); !ok {
		return
	}
	items := []ServiceStatus{{Service: "auth", Status: "healthy", Detail: "Identity database responded"}}
	if g.PlatformHealth != nil {
		items = append(items, g.PlatformHealth(r.Context())...)
	}
	writeJSON(w, 200, map[string]any{"items": items, "checked_at": time.Now().UTC().Format(time.RFC3339Nano)})
}
