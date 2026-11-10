package tests

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/example/runloom/workflows/internal/access"
	"github.com/example/runloom/workflows/internal/api"
	"github.com/example/runloom/workflows/internal/budget"
	"github.com/example/runloom/workflows/internal/prompts"
	"github.com/example/runloom/workflows/internal/services"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/example/runloom/workflows/internal/usage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func client(t *testing.T, endpoint, role, token string) *services.Client {
	t.Helper()
	path := t.TempDir() + "/token"
	require.NoError(t, os.WriteFile(path, []byte(token), 0600))
	c, err := services.NewClient(endpoint, role, path)
	require.NoError(t, err)
	return c
}
func TestServiceOwnershipAndRemoteBudgetAccounting(t *testing.T) {
	ctx := context.Background()
	team := uuid.NewString()
	workflowDB, err := store.New(t.TempDir())
	require.NoError(t, err)
	managementDB, err := store.New(t.TempDir())
	require.NoError(t, err)
	token := strings.Repeat("x", 40)
	digest := sha256.Sum256([]byte(token))
	trust := services.Trust{}
	for _, role := range []string{"worker", "workflows", "management", "auth"} {
		trust[role] = hex.EncodeToString(digest[:])
	}
	open := func(b store.Backend) func(*http.Request, string) (store.Backend, func(), error) {
		return func(_ *http.Request, id string) (store.Backend, func(), error) {
			if id != team {
				return nil, func() {}, access.ErrForbidden
			}
			return b, func() {}, nil
		}
	}
	workflows := httptest.NewServer(&services.Internal{Role: "workflows", Trust: trust, OpenTeam: open(workflowDB)})
	defer workflows.Close()
	management := httptest.NewServer(&services.Internal{Role: "management", Trust: trust, OpenTeam: open(managementDB)})
	defer management.Close()
	composed := &services.Backend{Backend: workflowDB, Role: "workflows", Team: team, Management: client(t, management.URL, "workflows", token)}
	managementView := &services.Backend{Backend: managementDB, Role: "management", Team: team, Workflows: client(t, workflows.URL, "management", token)}
	worker := &services.Backend{Backend: workflowDB, Role: "worker", Team: team, Management: client(t, management.URL, "worker", token)}
	p, err := managementDB.SavePrompt(ctx, prompts.Entry{ID: "summary", Name: "Summary", Template: prompts.Template{Text: "Hello"}}, true, "admin")
	require.NoError(t, err)
	remote, err := composed.GetPrompt(ctx, p.ID, 1)
	require.NoError(t, err)
	require.Equal(t, p, remote)
	_, err = workflowDB.GetPrompt(ctx, p.ID, 1)
	require.ErrorIs(t, err, store.ErrNotFound, "no prompt copied into workflow DB")
	_, err = composed.SavePrompt(ctx, p, false, "admin")
	require.ErrorIs(t, err, access.ErrForbidden)
	_, err = workflowDB.CreateProject(ctx, "A project")
	require.NoError(t, err)
	catalog, err := managementView.ProjectCatalog(ctx)
	require.NoError(t, err)
	require.Len(t, catalog.Projects, 2)
	row := usage.Entry{ID: uuid.NewString(), WorkflowID: "run", TemporalRunID: "temporal-run", Ancestors: []string{usage.RunKey("run", "temporal-run")}, Status: "pending", Accounting: "pending", StartedAt: "2026-10-01T00:00:00Z"}
	created, err := worker.BeginUsage(ctx, row)
	require.NoError(t, err)
	require.True(t, created)
	created, err = worker.BeginUsage(ctx, row)
	require.NoError(t, err)
	require.False(t, created, "network retry is idempotent")
	_, err = composed.BeginUsage(ctx, row)
	require.ErrorIs(t, err, access.ErrForbidden, "workflow API cannot manufacture usage")
	row.Status = "succeeded"
	row.Accounting = "estimated"
	row.CostUSD = "0.10"
	require.NoError(t, worker.FinishUsage(ctx, row))
	rows, err := composed.QueryUsage(ctx, usage.Filter{RunKey: usage.RunKey("run", "temporal-run")})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	empty, err := workflowDB.QueryUsage(ctx, usage.Filter{RunKey: usage.RunKey("run", "temporal-run")})
	require.NoError(t, err)
	require.Empty(t, empty)
	_, err = composed.SaveBudgetPolicy(ctx, budget.Policy{}, "admin")
	require.ErrorIs(t, err, access.ErrForbidden)
	composed.Team = uuid.NewString()
	_, err = composed.GetPrompt(ctx, p.ID, 1)
	require.ErrorIs(t, err, access.ErrForbidden)
	management.Close()
	_, err = worker.AggregateSpend(ctx, "2026-10-01", "2026-11-01")
	require.ErrorIs(t, err, services.ErrUnavailable)
}
func TestInternalAuthenticationAndOperationBoundaries(t *testing.T) {
	token := strings.Repeat("secret", 8)
	hash := sha256.Sum256([]byte(token))
	trust := services.Trust{"management": hex.EncodeToString(hash[:])}
	h := &services.Internal{Role: "workflows", Trust: trust, OpenTeam: func(*http.Request, string) (store.Backend, func(), error) {
		t.Fatal("forbidden request reached storage")
		return nil, nil, nil
	}}
	for _, tc := range []struct {
		role, token, path string
		status            int
	}{
		{"", "", "/internal/v1/teams/" + uuid.NewString() + "/definition/get", 401},
		{"worker", token, "/internal/v1/teams/" + uuid.NewString() + "/definition/get", 401},
		{"management", "wrong", "/internal/v1/teams/" + uuid.NewString() + "/definition/get", 401},
		{"management", token, "/internal/v1/teams/" + uuid.NewString() + "/definition/save", 403},
		{"management", token, "/internal/v1/teams/not-uuid/definition/get", 400},
		{"management", token, "/internal/v1/orgs/" + uuid.NewString() + "/secrets/audit", 403},
	} {
		r := httptest.NewRequest("POST", tc.path, strings.NewReader("{}"))
		r.Header.Set("X-Runloom-Service", tc.role)
		r.Header.Set("Authorization", "Bearer "+tc.token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		require.Equal(t, tc.status, w.Code)
	}
}
func TestPublicRouteOwnership(t *testing.T) {
	for path, owner := range map[string]string{
		"/api/v1/auth/config": "auth", "/api/v1/studio/me": "auth",
		"/api/v1/studio/orgs/o/teams/t/service-accounts":    "auth",
		"/api/v1/studio/orgs/o/teams/t/secrets":             "workflows",
		"/api/v1/studio/orgs/o/llm-analytics":               "management",
		"/api/v1/studio/orgs/o/teams/t/llm-budget-policies": "management",
		"/api/v1/studio/prompts":                            "management", "/api/v1/studio/prompts/test": "workflows",
		"/api/v1/studio/runs": "workflows", "/api/v1/runs": "workflows",
	} {
		require.Equal(t, owner, api.RouteOwner(path))
	}
	// Destination services reject routes they do not own before touching identity/storage.
	g := &api.Gateway{Role: "management"}
	w := httptest.NewRecorder()
	g.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/studio/orgs", nil))
	require.Equal(t, 404, w.Code)
}
