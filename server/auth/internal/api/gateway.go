package api

import (
	"context"
	"errors"
	"fmt"
	"github.com/example/runloom/auth/internal/access"
	"github.com/example/runloom/auth/internal/consumer"
	"github.com/example/runloom/auth/internal/store"
	runloomauth "github.com/example/runloom/sdk/go-auth"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type principalKey struct{}
type TeamOpener func(context.Context, string) (store.Backend, error)
type tenantServer struct{ backend store.Backend }
type Gateway struct {
	Directory          *access.Directory
	Verifier           access.Verifier
	PlatformVerifier   access.Verifier
	Config             access.Config
	OpenTeam           TeamOpener
	ConsumerProvider   consumer.Provider
	OrganizationEvents func(context.Context, string) ([]access.Event, error)
	PlatformHealth     func(context.Context) []ServiceStatus
	management         *http.ServeMux
	mu                 sync.Mutex
	teams              map[string]tenantServer
	closed             bool
}

func NewServiceGateway(d *access.Directory, v access.Verifier, c access.Config, open TeamOpener) *Gateway {
	g := &Gateway{Directory: d, Verifier: v, Config: c, OpenTeam: open, teams: map[string]tenantServer{}}
	g.management = http.NewServeMux()
	g.registerPlatformRoutes()
	g.management.HandleFunc("GET /api/v1/studio/me", g.me)
	g.management.HandleFunc("POST /api/v1/studio/orgs", g.createOrg)
	g.management.HandleFunc("GET /api/v1/studio/orgs/creation", g.organizationCreation)
	g.management.HandleFunc("GET /api/v1/studio/orgs/{org}/hierarchy", g.organizationHierarchy)
	g.management.HandleFunc("PATCH /api/v1/studio/orgs/{org}", g.renameOrganization)
	g.management.HandleFunc("POST /api/v1/studio/orgs/{org}/teams", g.createTeam)
	g.management.HandleFunc("GET /api/v1/studio/orgs/{org}/members", g.members)
	g.management.HandleFunc("POST /api/v1/studio/orgs/{org}/members", g.setMember)
	g.management.HandleFunc("DELETE /api/v1/studio/orgs/{org}/members/{email}", g.removeMember)
	g.management.HandleFunc("GET /api/v1/studio/orgs/{org}/teams/{team}/members", g.members)
	g.management.HandleFunc("POST /api/v1/studio/orgs/{org}/teams/{team}/members", g.setMember)
	g.management.HandleFunc("DELETE /api/v1/studio/orgs/{org}/teams/{team}/members/{email}", g.removeMember)
	g.management.HandleFunc("GET /api/v1/studio/orgs/{org}/audit", g.orgAudit)
	g.management.HandleFunc("GET /api/v1/studio/orgs/{org}/teams/{team}/service-accounts", g.serviceAccounts)
	g.management.HandleFunc("POST /api/v1/studio/orgs/{org}/teams/{team}/service-accounts", g.serviceAccounts)
	g.management.HandleFunc("PUT /api/v1/studio/orgs/{org}/teams/{team}/service-accounts/{account}", g.serviceAccounts)
	g.management.HandleFunc("POST /api/v1/studio/orgs/{org}/teams/{team}/service-accounts/{account}/{action}", g.serviceAccounts)

	return g
}
func (g *Gateway) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
	for _, t := range g.teams {
		_ = t.backend.Close(context.Background())
	}
}
func (g *Gateway) tenant(ctx context.Context, id string) (tenantServer, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return tenantServer{}, fmt.Errorf("server closed")
	}
	if t, ok := g.teams[id]; ok {
		return t, nil
	}
	b, err := g.OpenTeam(ctx, id)
	if err != nil {
		return tenantServer{}, err
	}
	t := tenantServer{backend: b}
	g.teams[id] = t
	return t, nil
}
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if RouteOwner(r.URL.Path) != "auth" {
		writeError(w, 404, fmt.Errorf("route not owned by this service"))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == "GET" && r.URL.Path == "/api/v1/platform/openapi.json" {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(platformOpenAPI)
		return
	}
	if r.Method == "GET" && r.URL.Path == "/api/v1/auth/config" {
		writeJSON(w, 200, map[string]any{"url": g.Config.URL, "realm": g.Config.Realm, "clientId": g.Config.ClientID, "googleEnabled": g.Config.GoogleEnabled})
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/v1/studio/") && !strings.HasPrefix(r.URL.Path, "/api/v1/platform/") {
		http.NotFound(w, r)
		return
	}
	token, tokenErr := runloomauth.Bearer(r)
	if tokenErr != nil {
		writeError(w, 401, fmt.Errorf("sign in required"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	u, err := g.Verifier.Verify(ctx, token)
	if err != nil {
		writeError(w, 401, fmt.Errorf("sign in with a verified email required"))
		return
	}
	ctx = access.WithBearer(ctx, token)
	if strings.HasPrefix(r.URL.Path, "/api/v1/platform/") && r.URL.Path != "/api/v1/platform/me" && g.PlatformVerifier != nil {
		if _, err := g.PlatformVerifier.Verify(ctx, token); err != nil {
			writeError(w, 403, fmt.Errorf("platform access requires a recent MFA session; ask the operator to configure platform authentication and sign in again"))
			return
		}
	}
	_, err = g.Directory.Bind(ctx, u)
	if err != nil {
		writeError(w, 503, fmt.Errorf("account directory unavailable"))
		return
	}
	r = r.WithContext(context.WithValue(r.Context(), principalKey{}, u))
	g.management.ServeHTTP(w, r)
}
func principal(r *http.Request) access.User { return r.Context().Value(principalKey{}).(access.User) }

func (g *Gateway) me(w http.ResponseWriter, r *http.Request) {
	s, _, err := g.Directory.Repo.Read(r.Context())
	if err != nil {
		writeError(w, 503, fmt.Errorf("directory unavailable"))
		return
	}
	writeJSON(w, 200, access.View(s, principal(r)))
}

func (g *Gateway) createOrg(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Name     string `json:"name"`
		ParentID string `json:"parent_id"`
	}
	if err := decodeJSON(w, r, &b); err != nil {
		writeError(w, 400, err)
		return
	}
	id, err := g.Directory.CreateOrganization(r.Context(), principal(r), b.Name, b.ParentID)
	if err != nil {
		accessError(w, err)
		return
	}
	writeJSON(w, 201, map[string]string{"id": id})
}

func (g *Gateway) createTeam(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &b); err != nil {
		writeError(w, 400, err)
		return
	}
	id, err := g.Directory.CreateTeam(r.Context(), principal(r), r.PathValue("org"), b.Name)
	if err != nil {
		accessError(w, err)
		return
	}
	writeJSON(w, 201, map[string]string{"id": id})
}

func (g *Gateway) members(w http.ResponseWriter, r *http.Request) {
	s, _, err := g.Directory.Repo.Read(r.Context())
	if err != nil {
		writeError(w, 503, fmt.Errorf("directory unavailable"))
		return
	}
	u := principal(r)
	org, team := r.PathValue("org"), r.PathValue("team")
	o, ok := s.Orgs[org]
	if !ok {
		accessError(w, access.ErrNotFound)
		return
	}
	type rosterMember struct {
		access.Member
		Inherited bool `json:"inherited,omitempty"`
	}
	m := map[string]rosterMember{}
	if team == "" {
		if !access.IsOrgAdmin(access.OrgRole(s, org, u.ID)) {
			accessError(w, access.ErrForbidden)
			return
		}
		for email, member := range o.Members {
			m[email] = rosterMember{Member: member}
		}
	} else {
		t, ok := o.Teams[team]
		actualOrg, role := access.TeamRole(s, team, u.ID)
		if !ok || actualOrg != org || (role != "admin" && role != "editor" && role != "viewer") {
			accessError(w, access.ErrForbidden)
			return
		}
		for email, member := range t.Members {
			m[email] = rosterMember{Member: member}
		}
		for email, member := range o.Members {
			if access.IsOrgAdmin(member.Role) && member.UserID != "" {
				member.Role = "admin"
				m[email] = rosterMember{Member: member, Inherited: true}
			}
		}
	}
	members := []rosterMember{}
	for _, v := range m {
		members = append(members, v)
	}
	sort.Slice(members, func(i, j int) bool { return members[i].Email < members[j].Email })
	writeJSON(w, 200, members)
}

func (g *Gateway) setMember(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := decodeJSON(w, r, &b); err != nil {
		writeError(w, 400, err)
		return
	}
	err := g.Directory.SetMember(r.Context(), principal(r), r.PathValue("org"), r.PathValue("team"), b.Email, b.Role, false)
	if err != nil {
		accessError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"saved": true})
}

func (g *Gateway) removeMember(w http.ResponseWriter, r *http.Request) {
	err := g.Directory.SetMember(r.Context(), principal(r), r.PathValue("org"), r.PathValue("team"), r.PathValue("email"), "", true)
	if err != nil {
		accessError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"removed": true})
}

func (g *Gateway) orgAudit(w http.ResponseWriter, r *http.Request) {
	s, _, err := g.Directory.Repo.Read(r.Context())
	if err != nil {
		writeError(w, 503, fmt.Errorf("directory unavailable"))
		return
	}
	org := r.PathValue("org")
	if !access.IsOrgAdmin(access.OrgRole(s, org, principal(r).ID)) {
		accessError(w, access.ErrForbidden)
		return
	}
	events := []access.Event{}
	for _, e := range s.Events {
		if e.OrgID == org {
			events = append(events, e)
		}
	}
	if g.OrganizationEvents != nil {
		extra, err := g.OrganizationEvents(r.Context(), org)
		if err != nil {
			writeError(w, 503, fmt.Errorf("organization audit unavailable"))
			return
		}
		for _, event := range extra {
			if event.OrgID == org {
				events = append(events, event)
			}
		}
		sort.SliceStable(events, func(i, j int) bool { return events[i].CreatedAt < events[j].CreatedAt })
	}
	writeJSON(w, 200, events)
}

func accessError(w http.ResponseWriter, err error) {
	code := 503
	var validation *access.ValidationError
	if errors.As(err, &validation) {
		code = 400
	}
	if errors.Is(err, access.ErrForbidden) {
		code = 403
	}
	if errors.Is(err, access.ErrNotFound) {
		code = 404
	}
	if code == 503 {
		err = fmt.Errorf("account directory unavailable; retry")
	}
	writeError(w, code, err)
}
