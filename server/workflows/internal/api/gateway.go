package api

import (
	"context"
	"errors"
	"fmt"
	runloomauth "github.com/example/runloom/sdk/go-auth"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/example/runloom/workflows/internal/access"
	"github.com/example/runloom/workflows/internal/consumer"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/example/runloom/workflows/internal/tenancy"
	"go.temporal.io/sdk/client"
)

type principalKey struct{}
type TeamOpener func(context.Context, string) (store.Backend, error)
type tenantServer struct {
	server  *Server
	backend store.Backend
}
type Gateway struct {
	Namespaces         *tenancy.Pool
	Secrets            *access.SecretVault
	ConsumerVerifier   consumer.Verifier
	ConsumerStore      consumer.Repository
	ConsumerProvider   consumer.Provider
	TemporalNamespace  string
	Base               *Server
	Directory          *access.Directory
	Verifier           access.Verifier
	Config             access.Config
	OpenTeam           TeamOpener
	temporal           client.Client
	queue              string
	management         *http.ServeMux
	mu                 sync.Mutex
	teams              map[string]tenantServer
	done               chan struct{}
	closed             bool
	Role               string
	OrganizationEvents func(context.Context, string) ([]access.Event, error)
}

func NewGateway(base *Server, d *access.Directory, v access.Verifier, c access.Config, open TeamOpener, temporal client.Client, queue string) *Gateway {
	return NewServiceGateway("", base, d, v, c, open, temporal, queue)
}

func NewServiceGateway(role string, base *Server, d *access.Directory, v access.Verifier, c access.Config, open TeamOpener, temporal client.Client, queue string, namespaces ...*tenancy.Pool) *Gateway {
	g := &Gateway{Base: base, Directory: d, Verifier: v, Config: c, OpenTeam: open, temporal: temporal, queue: queue, teams: map[string]tenantServer{}, done: make(chan struct{})}
	if len(namespaces) > 0 {
		g.Namespaces = namespaces[0]
	}
	g.Role = role
	g.management = http.NewServeMux()
	g.management.HandleFunc("GET /api/v1/studio/orgs/{org}/teams/{team}/connections", g.connections)
	g.management.HandleFunc("PUT /api/v1/studio/orgs/{org}/teams/{team}/connections/{connection}", g.connections)
	g.management.HandleFunc("DELETE /api/v1/studio/orgs/{org}/teams/{team}/connections/{connection}", g.connections)
	g.management.HandleFunc("POST /api/v1/studio/orgs/{org}/teams/{team}/connections/{connection}/{action}", g.connections)
	g.management.HandleFunc("GET /api/v1/studio/orgs/{org}/teams/{team}/connections/{connection}/schema", g.connections)
	g.management.HandleFunc("GET /api/v1/studio/orgs/{org}/teams/{team}/llm-budget-policies", g.budgetPolicies)
	g.management.HandleFunc("PUT /api/v1/studio/orgs/{org}/teams/{team}/llm-budget-policies/{policy}", g.budgetPolicies)
	g.management.HandleFunc("GET /api/v1/studio/orgs/{org}/llm-analytics", g.llmAnalytics)
	g.management.HandleFunc("GET /api/v1/studio/orgs/{org}/teams/{team}/secrets", g.secrets)
	g.management.HandleFunc("PUT /api/v1/studio/orgs/{org}/teams/{team}/secrets/{name}", g.secrets)
	g.management.HandleFunc("DELETE /api/v1/studio/orgs/{org}/teams/{team}/secrets/{name}", g.secrets)
	g.management.HandleFunc("GET /api/v1/studio/me", g.me)
	g.management.HandleFunc("POST /api/v1/studio/orgs", g.createOrg)
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
	if role == "" || role == "workflows" {
		go g.reconcileTeams()
	}
	return g
}
func (g *Gateway) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return
	}
	g.closed = true
	close(g.done)
	for _, t := range g.teams {
		t.server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = t.backend.Close(ctx)
		cancel()
	}
	g.Base.Close()
}
func (g *Gateway) tenant(ctx context.Context, id string) (tenantServer, error) {
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return tenantServer{}, fmt.Errorf("server closed")
	}
	if t, ok := g.teams[id]; ok {
		g.mu.Unlock()
		return t, nil
	}
	g.mu.Unlock()
	c, orgID, namespace := g.temporal, "", ""
	if g.Namespaces != nil {
		state, _, err := g.Directory.Repo.Read(ctx)
		if err != nil {
			return tenantServer{}, fmt.Errorf("organization directory unavailable")
		}
		for org, o := range state.Orgs {
			if _, ok := o.Teams[id]; ok {
				orgID = org
				break
			}
		}
		if orgID == "" {
			return tenantServer{}, access.ErrNotFound
		}
		c, namespace, err = g.Namespaces.Ensure(ctx, orgID)
		if err != nil {
			return tenantServer{}, fmt.Errorf("organization execution namespace not ready")
		}
	}
	b, err := g.OpenTeam(ctx, id)
	if err != nil {
		return tenantServer{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		_ = b.Close(ctx)
		return tenantServer{}, fmt.Errorf("server closed")
	}
	if t, ok := g.teams[id]; ok {
		_ = b.Close(ctx)
		return t, nil
	}
	t := tenantServer{server: New(b, c, g.queue), backend: b}
	t.server.orgID, t.server.namespace = orgID, namespace
	g.teams[id] = t
	return t, nil
}
func (g *Gateway) reconcileTeams() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		s, _, err := g.Directory.Repo.Read(ctx)
		if err == nil {
			for org, o := range s.Orgs {
				if g.Namespaces != nil {
					if _, _, err := g.Namespaces.Ensure(ctx, org); err != nil {
						continue
					}
				}
				for id := range o.Teams {
					select {
					case <-g.done:
						cancel()
						return
					default:
					}
					_, _ = g.tenant(ctx, id)
				}
			}
		}
		cancel()
		select {
		case <-g.done:
			return
		case <-ticker.C:
		}
	}
}
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if g.Role != "" && RouteOwner(r.URL.Path) != g.Role && !SharedRoute(r.URL.Path) {
		writeError(w, 404, fmt.Errorf("route not owned by this service"))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if serveAPIDocs(w, r) {
		return
	}
	if r.Method == "GET" && (r.URL.Path == "/healthz" || r.URL.Path == "/api/v1/health") {
		g.Base.Handler().ServeHTTP(w, r)
		return
	}
	if r.Method == "GET" && r.URL.Path == "/api/v1/auth/config" {
		writeJSON(w, 200, map[string]any{"url": g.Config.URL, "realm": g.Config.Realm, "clientId": g.Config.ClientID, "googleEnabled": g.Config.GoogleEnabled})
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/v1/studio/") {
		if r.URL.Path == "/api/v1" || strings.HasPrefix(r.URL.Path, "/api/v1/") {
			g.consumerRequest(w, r)
		} else {
			writeError(w, http.StatusNotFound, fmt.Errorf("API route not found; use a supported versioned endpoint"))
		}
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
	s, err := g.Directory.Bind(ctx, u)
	if err != nil {
		writeError(w, 503, fmt.Errorf("account directory unavailable"))
		return
	}
	r = r.WithContext(context.WithValue(r.Context(), principalKey{}, u))
	if r.URL.Path == "/api/v1/studio/me" || r.URL.Path == "/api/v1/studio/orgs" || strings.HasPrefix(r.URL.Path, "/api/v1/studio/orgs/") {
		g.management.ServeHTTP(w, r)
		return
	}
	team := r.Header.Get("X-Runloom-Team")
	_, role := access.TeamRole(s, team, u.ID)
	if role == "" {
		writeError(w, 403, fmt.Errorf("select a team you belong to"))
		return
	}
	if r.Method != "GET" && role == "viewer" {
		writeError(w, 403, fmt.Errorf("viewer access is read-only"))
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/studio/")
	if r.Method == "GET" && (path == "runs" || strings.HasPrefix(path, "runs/") || path == "audit" || path == "schedules" || strings.HasPrefix(path, "schedules/")) && !sensitiveRunAccess(role) {
		writeError(w, 403, fmt.Errorf("sensitive execution data access requires an authorized role"))
		return
	}
	t, err := g.tenant(ctx, team)
	if err != nil {
		writeError(w, 503, fmt.Errorf("team storage unavailable"))
		return
	}
	// Scope checks precede every direct Temporal lookup or signal.
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/studio/"), "/")
	if len(parts) >= 2 && (parts[0] == "runs" || parts[0] == "callbacks") {
		if _, err := t.backend.GetRun(parts[1]); err != nil {
			writeError(w, 404, fmt.Errorf("run not found in this team"))
			return
		}
	}
	r.Header.Set("X-Runloom-Actor", u.ID)
	t.server.Handler().ServeHTTP(w, r)
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
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &b); err != nil {
		writeError(w, 400, err)
		return
	}
	id, err := g.Directory.CreateOrg(r.Context(), principal(r), b.Name)
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
	m := o.Members
	if team == "" {
		if !access.IsOrgAdmin(access.OrgRole(s, org, u.ID)) {
			accessError(w, access.ErrForbidden)
			return
		}
	} else {
		t, ok := o.Teams[team]
		_, role := access.TeamRole(s, team, u.ID)
		if !ok || role != "admin" {
			accessError(w, access.ErrForbidden)
			return
		}
		m = t.Members
	}
	members := []access.Member{}
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
