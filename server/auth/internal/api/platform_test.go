package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/runloom/auth/internal/access"
	"github.com/stretchr/testify/require"
)

func TestPlatformBoundariesAndRevocation(t *testing.T) {
	g := membershipGateway(t)
	ctx := t.Context()
	user := func(id string) access.User { return access.User{ID: id, Email: id + "@example.test"} }
	require.NoError(t, g.Directory.BootstrapPlatformAdmin(ctx, user("owner")))
	path := "/api/v1/platform/"
	for _, actor := range []string{"org-admin", "team-admin", "editor", "viewer", "outsider"} {
		for _, endpoint := range []string{"organizations", "staff", "audit", "health"} {
			require.Equal(t, 403, rosterRequest(g, actor, "GET", path+endpoint, "").Code, actor+" "+endpoint)
		}
	}
	require.Equal(t, 401, rosterRequest(g, "", "GET", path+"organizations", "").Code)
	require.Equal(t, 401, rosterRequest(g, "machine", "GET", path+"me", "").Code)
	require.Equal(t, 200, rosterRequest(g, "viewer", "GET", path+"me", "").Code)
	change := func(actor, target, role string) int {
		b, _ := json.Marshal(map[string]string{"email": target + "@example.test", "role": role, "reason": "Platform access regression test"})
		return rosterRequest(g, actor, "PUT", path+"staff", string(b)).Code
	}
	require.Equal(t, 200, change("owner", "outsider", access.PlatformStaff))
	for _, endpoint := range []string{"organizations", "health"} {
		require.Equal(t, 200, rosterRequest(g, "outsider", "GET", path+endpoint, "").Code)
	}
	for _, endpoint := range []string{"staff", "audit"} {
		require.Equal(t, 403, rosterRequest(g, "outsider", "GET", path+endpoint, "").Code)
	}
	require.Equal(t, 403, change("outsider", "outsider", access.PlatformAdmin))
	require.Equal(t, 403, rosterRequest(g, "outsider", "GET", "/api/v1/studio/orgs/org/members", "").Code)
	response := rosterRequest(g, "outsider", "GET", path+"organizations?limit=1", "")
	require.Equal(t, 200, response.Code)
	require.NotContains(t, response.Body.String(), "@example.test")
	require.NotContains(t, response.Body.String(), "platform_roles")
	require.NotContains(t, response.Body.String(), "secrets")
	var page struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
	require.Len(t, page.Items, 1)
	require.Equal(t, 2, page.Total)
	keys := []string{}
	for key := range page.Items[0] {
		keys = append(keys, key)
	}
	require.ElementsMatch(t, []string{"id", "name", "team_count", "member_count", "child_count", "child_limit"}, keys)
	policy := page.Items[0]["child_limit"].(map[string]any)
	require.Len(t, policy, 4)
	require.Equal(t, 400, rosterRequest(g, "owner", "GET", path+"audit?limit=999", "").Code)
	require.Equal(t, 400, rosterRequest(g, "owner", "GET", path+"audit?offset=-1", "").Code)
	require.Equal(t, 400, change("owner", "owner", ""), "last administrator")
	require.Equal(t, 200, change("owner", "outsider", ""))
	require.Equal(t, 403, rosterRequest(g, "outsider", "GET", path+"health", "").Code, "same token must lose access immediately")
	s, _, err := g.Directory.Repo.ReadOwned(ctx)
	require.NoError(t, err)
	require.Equal(t, "member", access.OrgRole(s, "other-org", "outsider"))
	require.Len(t, s.PlatformEvents, 3)
	require.Equal(t, "owner", s.PlatformEvents[2].Actor)
	for _, event := range s.Events {
		require.False(t, strings.HasPrefix(event.Action, "platform."))
	}
	// A platform admin without tenant membership is not a tenant administrator.
	require.Equal(t, 200, change("owner", "outsider", access.PlatformAdmin))
	require.Equal(t, 403, rosterRequest(g, "outsider", "GET", "/api/v1/studio/orgs/org/members", "").Code)
	// Explicit re-running of setup never resurrects a revoked admin.
	require.Equal(t, 200, change("owner", "owner", ""))
	require.NoError(t, g.Directory.BootstrapPlatformAdmin(ctx, user("owner")))
	require.Equal(t, 403, rosterRequest(g, "owner", "GET", path+"staff", "").Code)
}

func TestPlatformDoesNotTrustCallerRoleOrActorHeaders(t *testing.T) {
	g := membershipGateway(t)
	req := httptest.NewRequest("GET", "/api/v1/platform/staff", nil)
	req.Header.Set("Authorization", "Bearer viewer")
	req.Header.Set("X-Runloom-Role", "platform_admin")
	req.Header.Set("X-Runloom-Actor", "owner")
	response := httptest.NewRecorder()
	g.ServeHTTP(response, req)
	require.Equal(t, 403, response.Code)
	g.PlatformHealth = func(context.Context) []ServiceStatus { t.Fatal("unauthorized health probing"); return nil }
	require.Equal(t, 403, rosterRequest(g, "viewer", "GET", "/api/v1/platform/health", "").Code)
}
