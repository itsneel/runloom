package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/example/runloom/workflows/internal/access"
	"github.com/example/runloom/workflows/internal/consumer"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/example/runloom/workflows/internal/usage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type analyticsFixture struct {
	g                  *Gateway
	d                  *access.Directory
	users              testVerifier
	org, team, private string
}

func newAnalyticsFixture(t *testing.T) analyticsFixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	repo, err := access.Open(ctx, "file", "", "", root)
	require.NoError(t, err)
	t.Cleanup(func() { repo.Close(ctx) })
	d := &access.Directory{Repo: repo}
	users := testVerifier{}
	for _, id := range []string{"owner", "orgadmin", "viewer", "editor", "admin", "outsider"} {
		users[id] = access.User{ID: id, Email: id + "@example.test"}
		_, err = d.Bind(ctx, users[id])
		require.NoError(t, err)
	}
	org, err := d.CreateOrg(ctx, users["owner"], "Organization")
	require.NoError(t, err)
	s, _, err := repo.Read(ctx)
	require.NoError(t, err)
	team := ""
	for id := range s.Orgs[org].Teams {
		team = id
	}
	private, err := d.CreateTeam(ctx, users["owner"], org, "Private")
	require.NoError(t, err)
	require.NoError(t, d.SetMember(ctx, users["owner"], org, "", users["orgadmin"].Email, "admin", false))
	for _, role := range []string{"viewer", "editor", "admin"} {
		require.NoError(t, d.SetMember(ctx, users["owner"], org, "", users[role].Email, "member", false))
		require.NoError(t, d.SetMember(ctx, users["owner"], org, team, users[role].Email, role, false))
	}
	base, err := store.New(filepath.Join(root, "base"))
	require.NoError(t, err)
	g := NewGateway(New(base, nil, "queue"), d, users, access.Config{}, func(ctx context.Context, id string) (store.Backend, error) {
		return store.OpenTeam(ctx, "file", "", "", root, id)
	}, nil, "queue")
	t.Cleanup(g.Close)
	g.ConsumerVerifier = machineVerifier{"machine": consumer.Identity{}}
	for _, id := range []string{team, private} {
		tenant, err := g.tenant(ctx, id)
		require.NoError(t, err)
		for i := 1; i <= 60; i++ {
			e := usage.Entry{ID: uuid.NewString(), TeamID: "untrusted-payload", WorkflowID: fmt.Sprintf("run-%02d", i), TemporalRunID: "execution", DefinitionID: fmt.Sprintf("workflow-%02d", i), StartedAt: time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339Nano), Status: "pending", Accounting: "pending", IsTest: i == 1}
			_, err = tenant.backend.BeginUsage(ctx, e)
			require.NoError(t, err)
			e.Status = "succeeded"
			e.Accounting = "estimated"
			e.CostUSD = fmt.Sprint(i)
			require.NoError(t, tenant.backend.FinishUsage(ctx, e))
		}
	}
	return analyticsFixture{g, d, users, org, team, private}
}
func (f analyticsFixture) call(token, query string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/api/v1/studio/orgs/"+f.org+"/llm-analytics?period=1d"+query, nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	r.Header.Set("X-Runloom-Team", f.private) // never trusted as an analytics grant
	w := httptest.NewRecorder()
	f.g.ServeHTTP(w, r)
	return w
}

type analyticsResponse struct {
	Summary usage.Metrics        `json:"summary"`
	Rows    []usage.AnalyticsRow `json:"rows"`
	Allowed []access.TeamView    `json:"allowed_teams"`
	Total   int                  `json:"total_rows"`
}

func TestLLMAnalyticsAuthorizationSortingAndPagination(t *testing.T) {
	f := newAnalyticsFixture(t)
	for _, role := range []string{"viewer", "editor", "admin", "owner", "orgadmin"} {
		w := f.call(role, "")
		require.Equal(t, 200, w.Code, w.Body.String())
		var result analyticsResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		teams := 1
		cost := "1830.000000000000"
		if role == "owner" || role == "orgadmin" {
			teams = 2
			cost = "3660.000000000000"
		}
		require.Len(t, result.Allowed, teams)
		require.Equal(t, 60*teams, result.Total)
		require.Equal(t, cost, result.Summary.EstimatedUSD)
		require.Len(t, result.Rows, 50)
		require.Equal(t, "60.000000000000", result.Rows[0].EstimatedUSD)
		if role != "owner" && role != "orgadmin" {
			for _, row := range result.Rows {
				require.Equal(t, f.team, row.TeamID)
			}
		}
	}
	for _, token := range []string{"", "invalid", "machine"} {
		require.Equal(t, 401, f.call(token, "").Code)
	}
	require.Equal(t, 403, f.call("outsider", "").Code)
	require.Equal(t, 403, f.call("viewer", "&team="+f.private).Code)
	w := f.call("viewer", "&page=1")
	var result analyticsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Len(t, result.Rows, 10)
	require.Equal(t, "10.000000000000", result.Rows[0].EstimatedUSD)
	w = f.call("owner", "&team="+f.team+"&sort=cost&direction=asc")
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Equal(t, "1.000000000000", result.Rows[0].EstimatedUSD)
	w = f.call("owner", "&group=teams")
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Len(t, result.Rows, 2)
	w = f.call("viewer", "&mode=test")
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Equal(t, 1, result.Summary.Requests)
	w = f.call("viewer", "&mode=production")
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Equal(t, 59, result.Summary.Requests)
	for _, q := range []string{"&sort=bad", "&direction=bad", "&mode=bad", "&group=bad", "&page=-1", "&as_of=bad", "&as_of=2000-01-01T00:00:00Z"} {
		require.Equal(t, 400, f.call("owner", q).Code, q)
	}
	require.NoError(t, f.d.SetMember(context.Background(), f.users["owner"], f.org, f.team, f.users["viewer"].Email, "", true))
	require.Equal(t, 403, f.call("viewer", "&team="+f.team).Code)
	w = f.call("viewer", "")
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Zero(t, result.Summary.Requests)
	require.Empty(t, result.Allowed)
}

type failingAnalyticsStore struct{ store.Backend }

func (b failingAnalyticsStore) QueryUsage(context.Context, usage.Filter) ([]usage.Entry, error) {
	return nil, fmt.Errorf("database offline")
}
func TestLLMAnalyticsDoesNotReturnPartialTotals(t *testing.T) {
	f := newAnalyticsFixture(t)
	tenant, err := f.g.tenant(context.Background(), f.private)
	require.NoError(t, err)
	f.g.mu.Lock()
	f.g.teams[f.private] = tenantServer{server: tenant.server, backend: failingAnalyticsStore{tenant.backend}}
	f.g.mu.Unlock()
	w := f.call("owner", "")
	require.Equal(t, 503, w.Code)
	require.NotContains(t, w.Body.String(), "estimated_usd")
}

type oversizedAnalyticsStore struct{ store.Backend }

func (b oversizedAnalyticsStore) QueryUsage(_ context.Context, f usage.Filter) ([]usage.Entry, error) {
	rows := make([]usage.Entry, f.Limit)
	for i := range rows {
		rows[i] = usage.Entry{ID: uuid.NewString(), StartedAt: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)}
	}
	return rows, nil
}
func TestLLMAnalyticsRejectsOversizedReports(t *testing.T) {
	f := newAnalyticsFixture(t)
	tenant, err := f.g.tenant(context.Background(), f.team)
	require.NoError(t, err)
	f.g.mu.Lock()
	f.g.teams[f.team] = tenantServer{server: tenant.server, backend: oversizedAnalyticsStore{tenant.backend}}
	f.g.mu.Unlock()
	w := f.call("viewer", "")
	require.Equal(t, 422, w.Code)
	require.NotContains(t, w.Body.String(), "estimated_usd")
	require.Contains(t, w.Body.String(), "50,000")
}

type revokingAnalyticsStore struct {
	store.Backend
	revoke func()
}

func (b revokingAnalyticsStore) QueryUsage(ctx context.Context, f usage.Filter) ([]usage.Entry, error) {
	b.revoke()
	return b.Backend.QueryUsage(ctx, f)
}
func TestLLMAnalyticsRevocationDuringRead(t *testing.T) {
	f := newAnalyticsFixture(t)
	tenant, err := f.g.tenant(context.Background(), f.team)
	require.NoError(t, err)
	f.g.mu.Lock()
	f.g.teams[f.team] = tenantServer{server: tenant.server, backend: revokingAnalyticsStore{tenant.backend, func() {
		require.NoError(t, f.d.SetMember(context.Background(), f.users["owner"], f.org, f.team, f.users["viewer"].Email, "", true))
	}}}
	f.g.mu.Unlock()
	w := f.call("viewer", "")
	require.Equal(t, 403, w.Code)
	require.NotContains(t, w.Body.String(), "estimated_usd")
}
