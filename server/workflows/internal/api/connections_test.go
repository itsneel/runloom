package api

import (
	"context"
	"encoding/json"
	"github.com/example/runloom/workflows/internal/access"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConnectionPermissionsEncryptionAndRotation(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	repo, err := access.Open(ctx, "file", "", "", root)
	require.NoError(t, err)
	defer repo.Close(ctx)
	d := &access.Directory{Repo: repo}
	users := testVerifier{}
	for _, role := range []string{"admin", "editor", "viewer", "outsider"} {
		users[role] = access.User{ID: role, Email: role + "@example.test"}
		_, err = d.Bind(ctx, users[role])
		require.NoError(t, err)
	}
	org, err := d.CreateOrg(ctx, users["admin"], "Connections test")
	require.NoError(t, err)
	s, _, err := repo.Read(ctx)
	require.NoError(t, err)
	team := ""
	for id := range s.Orgs[org].Teams {
		team = id
	}
	for _, role := range []string{"editor", "viewer"} {
		require.NoError(t, d.SetMember(ctx, users["admin"], org, "", users[role].Email, "member", false))
		require.NoError(t, d.SetMember(ctx, users["admin"], org, team, users[role].Email, role, false))
	}
	b, err := store.New(filepath.Join(root, "base"))
	require.NoError(t, err)
	g := NewGateway(New(b, nil, "test"), d, users, access.Config{}, func(ctx context.Context, id string) (store.Backend, error) {
		return store.OpenTeam(ctx, "file", "", "", root, id)
	}, nil, "test")
	defer g.Close()
	key := filepath.Join(root, "key")
	require.NoError(t, os.WriteFile(key, []byte(strings.Repeat("k", 32)), 0600))
	g.Secrets, err = access.NewSecretVault(d, key)
	require.NoError(t, err)
	endpoint := "/api/v1/studio/orgs/" + org + "/teams/" + team + "/connections"
	call := func(method, path, user, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+user)
		w := httptest.NewRecorder()
		g.ServeHTTP(w, r)
		return w
	}
	body := `{"name":"Reporting","provider":"postgres","host":"db.example.test","port":5432,"database":"reporting","username":"reader","password":"synthetic-connection-password","tls":true,"access":"read","enabled":true}`
	require.Equal(t, 403, call("PUT", endpoint+"/reporting", "editor", body).Code)
	require.Equal(t, 403, call("GET", endpoint, "outsider", "").Code)
	require.Equal(t, 401, call("GET", endpoint, "machine", "").Code)
	w := call("PUT", endpoint+"/reporting", "admin", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	for _, role := range []string{"viewer", "editor", "admin"} {
		w = call("GET", endpoint, role, "")
		require.Equal(t, 200, w.Code)
		require.Contains(t, w.Body.String(), "Reporting")
		require.NotContains(t, w.Body.String(), "password")
		require.NotContains(t, w.Body.String(), "ciphertext")
	}
	require.Equal(t, 403, call("POST", endpoint+"/reporting/test", "viewer", "").Code)
	require.Equal(t, 403, call("GET", endpoint+"/reporting/schema", "viewer", "").Code)
	require.Equal(t, 403, call("DELETE", endpoint+"/reporting", "editor", "").Code)
	raw, err := os.ReadFile(filepath.Join(root, "_secrets.json"))
	require.NoError(t, err)
	require.NotContains(t, string(raw), "synthetic-connection-password")
	w = call("PUT", endpoint+"/reporting", "admin", strings.Replace(body, `"synthetic-connection-password"`, `""`, 1))
	require.Equal(t, 200, w.Code)
	c, err := g.Secrets.ResolveConnection(ctx, team, "reporting")
	require.NoError(t, err)
	require.True(t, c.Password == "synthetic-connection-password")
	w = call("PUT", endpoint+"/reporting", "admin", strings.Replace(body, "synthetic-connection-password", "synthetic-rotated", 1))
	require.Equal(t, 200, w.Code)
	c, err = g.Secrets.ResolveConnection(ctx, team, "reporting")
	require.NoError(t, err)
	require.True(t, c.Password == "synthetic-rotated")
	_, err = g.Secrets.ResolveConnection(ctx, "other-team", "reporting")
	require.Error(t, err)
	s, _, err = repo.Read(ctx)
	require.NoError(t, err)
	audit, _ := json.Marshal(s.Events)
	require.NotContains(t, string(audit), "synthetic")
	require.Equal(t, 400, call("PUT", endpoint+"/bad%20name", "admin", body).Code)
	require.Equal(t, 200, call("DELETE", endpoint+"/reporting", "admin", "").Code)
	_, err = g.Secrets.ResolveConnection(ctx, team, "reporting")
	require.Error(t, err)
}
