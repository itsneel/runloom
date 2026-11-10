package api

import (
	"context"
	"errors"
	"fmt"
	runloomauth "github.com/example/runloom/sdk/go-auth"
	"net/http"

	"strings"
	"sync"
	"time"

	"github.com/example/runloom/management/internal/access"

	"github.com/example/runloom/management/internal/store"
)

type principalKey struct{}
type TeamOpener func(context.Context, string) (store.View, error)
type tenantServer struct {
	server  *Server
	backend store.View
}
type Gateway struct {
	Directory  *access.Directory
	Verifier   access.Verifier
	Config     access.Config
	OpenTeam   TeamOpener
	management *http.ServeMux
	mu         sync.Mutex
	teams      map[string]tenantServer
	closed     bool
}

func NewServiceGateway(d *access.Directory, v access.Verifier, c access.Config, open TeamOpener) *Gateway {
	g := &Gateway{Directory: d, Verifier: v, Config: c, OpenTeam: open, teams: map[string]tenantServer{}, management: http.NewServeMux()}
	g.management.HandleFunc("GET /api/v1/studio/orgs/{org}/teams/{team}/llm-budget-policies", g.budgetPolicies)
	g.management.HandleFunc("PUT /api/v1/studio/orgs/{org}/teams/{team}/llm-budget-policies/{policy}", g.budgetPolicies)
	g.management.HandleFunc("GET /api/v1/studio/orgs/{org}/llm-analytics", g.llmAnalytics)
	return g
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
	t := tenantServer{server: New(b), backend: b}
	g.teams[id] = t
	return t, nil
}
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if RouteOwner(r.URL.Path) != "management" {
		writeError(w, 404, fmt.Errorf("route not owned by this service"))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
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
	t, err := g.tenant(ctx, team)
	if err != nil {
		writeError(w, 503, fmt.Errorf("team storage unavailable"))
		return
	}
	r.Header.Set("X-Runloom-Actor", u.ID)
	t.server.Handler().ServeHTTP(w, r)
}
func principal(r *http.Request) access.User { return r.Context().Value(principalKey{}).(access.User) }
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
func (g *Gateway) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
	for _, t := range g.teams {
		_ = t.backend.Close(context.Background())
	}
}
