package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/runloom/workflows/internal/access"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/stretchr/testify/require"
)

func TestWorkflowSecretsAuthorizationEncryptionAndRotation(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	repo, err := access.Open(ctx, "file", "", "", root)
	require.NoError(t, err)
	defer repo.Close(ctx)
	d := &access.Directory{Repo: repo}
	users := testVerifier{"admin": {ID: "admin", Email: "admin@example.com"}, "editor": {ID: "editor", Email: "editor@example.com"}, "outsider": {ID: "outsider", Email: "other@example.com"}}
	for _, u := range users {
		_, err = d.Bind(ctx, u)
		require.NoError(t, err)
	}
	org, err := d.CreateOrg(ctx, users["admin"], "Secrets test")
	require.NoError(t, err)
	s, _, err := repo.Read(ctx)
	require.NoError(t, err)
	team := ""
	for id := range s.Orgs[org].Teams {
		team = id
	}
	require.NoError(t, d.SetMember(ctx, users["admin"], org, "", users["editor"].Email, "member", false))
	require.NoError(t, d.SetMember(ctx, users["admin"], org, team, users["editor"].Email, "editor", false))
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
	endpoint := "/api/v1/studio/orgs/" + org + "/teams/" + team + "/secrets"
	call := func(method, path, user, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+user)
		w := httptest.NewRecorder()
		g.ServeHTTP(w, r)
		return w
	}
	body := `{"value":"synthetic-never-expose"}`
	require.Equal(t, 403, call("PUT", endpoint+"/api-key", "editor", body).Code)
	require.Equal(t, 403, call("GET", endpoint, "outsider", "").Code)
	require.Equal(t, 401, call("GET", endpoint, "machine", "").Code)
	w := call("PUT", endpoint+"/api-key", "admin", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "synthetic")
	w = call("GET", endpoint, "editor", "")
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), "api-key")
	require.NotContains(t, w.Body.String(), "ciphertext")
	require.NotContains(t, w.Body.String(), "origin")
	require.NotContains(t, w.Body.String(), "synthetic")
	value, err := g.Secrets.Resolve(ctx, team, "api-key")
	require.NoError(t, err)
	require.Equal(t, "synthetic-never-expose", value)
	_, err = g.Secrets.Resolve(ctx, "other-team", "api-key")
	require.Error(t, err)
	stored, err := os.ReadFile(filepath.Join(root, "_secrets.json"))
	require.NoError(t, err)
	require.NotContains(t, string(stored), "synthetic-never-expose")
	s, _, err = repo.Read(ctx)
	require.NoError(t, err)
	audit, _ := json.Marshal(s.Events)
	require.NotContains(t, string(audit), "synthetic")
	otherKey := filepath.Join(root, "wrong-key")
	require.NoError(t, os.WriteFile(otherKey, []byte(strings.Repeat("z", 32)), 0600))
	wrong, err := access.NewSecretVault(d, otherKey)
	require.NoError(t, err)
	_, err = wrong.Resolve(ctx, team, "api-key")
	require.Error(t, err)
	require.Equal(t, 200, call("PUT", endpoint+"/api-key", "admin", strings.ReplaceAll(body, "synthetic-never-expose", "replacement-value")).Code)
	value, err = g.Secrets.Resolve(ctx, team, "api-key")
	require.NoError(t, err)
	require.Equal(t, "replacement-value", value)
	require.Equal(t, 200, call("DELETE", endpoint+"/api-key", "admin", "").Code)
	_, err = g.Secrets.Resolve(ctx, team, "api-key")
	require.Error(t, err)
	for _, invalid := range []string{`{}`, `{"value":""}`, `{"value":"line\nbreak"}`, `{"value":"` + strings.Repeat("x", 8193) + `"}`} {
		w = call("PUT", endpoint+"/api-key", "admin", invalid)
		require.Equal(t, 400, w.Code)
	}
	require.Equal(t, 400, call("PUT", endpoint+"/INVALID", "admin", body).Code)
	g.Secrets = nil
	w = call("GET", endpoint, "admin", "")
	require.Contains(t, w.Body.String(), `"configured":false`)
	w = call("PUT", endpoint+"/api-key", "admin", body)
	require.Equal(t, 503, w.Code)
	require.Contains(t, w.Body.String(), "service operation failed")
	require.NotContains(t, w.Body.String(), "synthetic-never-expose")
}
