package localadmin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/example/runloom/auth/internal/access"
	"github.com/example/runloom/auth/internal/config"
	"github.com/stretchr/testify/require"
)

func TestLocalGuards(t *testing.T) {
	settings := config.Settings{DatabaseDriver: "mongodb", DatabaseURL: "mongodb://mongodb:27017/runloom"}
	require.NoError(t, Validate("local", "1", "http://keycloak:8080/auth", settings))
	for _, env := range []string{"", "development", "staging", "production"} {
		require.Error(t, Validate(env, "1", "http://keycloak:8080/auth", settings))
	}
	require.Error(t, Validate("local", "", "http://keycloak:8080/auth", settings))
	for _, address := range []string{"http://example.com/auth", "https://keycloak/auth", "http://keycloak@evil/auth", "http://keycloak/auth?redirect=evil", "http://keycloak/auth#fragment"} {
		require.Error(t, Validate("local", "1", address, settings))
	}
	for _, address := range []string{"mongodb+srv://mongodb/runloom", "mongodb://remote:27017/runloom", "mongodb://mongodb,remote/runloom", "mongodb://mongodb/runloom?replicaSet=remote"} {
		settings.DatabaseURL = address
		require.Error(t, Validate("local", "1", "http://keycloak:8080/auth", settings))
	}
	settings.DatabaseDriver = "postgres"
	settings.DatabaseURL = "postgresql://runloom:runloom@postgres:5432/runloom"
	require.NoError(t, Validate("local", "1", "http://localhost:18081/auth", settings))
	settings.DatabaseURL = "postgresql://remote/runloom"
	require.Error(t, Validate("local", "1", "http://localhost:18081/auth", settings))
	settings.DatabaseDriver = "file"
	require.Error(t, Validate("local", "1", "http://localhost:18081/auth", settings))
}

func TestKubernetesLocalGuards(t *testing.T) {
	settings := config.Settings{DatabaseDriver: "mongodb", DatabaseURL: "mongodb://runloom-database:27017/runloom"}
	require.NoError(t, ValidateKubernetes("local", "1", "http://runloom-keycloak:8080/auth", settings, "runloom"))
	require.Error(t, ValidateKubernetes("production", "1", "http://runloom-keycloak:8080/auth", settings, "runloom"))
	require.Error(t, ValidateKubernetes("local", "", "http://runloom-keycloak:8080/auth", settings, "runloom"))
	require.Error(t, ValidateKubernetes("local", "1", "http://remote/auth", settings, "runloom"))
	require.Error(t, ValidateKubernetes("local", "1", "http://runloom-keycloak:8080/auth", settings, "wrong"))
	for _, address := range []string{"mongodb://remote/runloom", "mongodb://runloom-database,remote/runloom", "mongodb://runloom-database/runloom?replicaSet=other"} {
		settings.DatabaseURL = address
		require.Error(t, ValidateKubernetes("local", "1", "http://runloom-keycloak:8080/auth", settings, "runloom"))
	}
	settings.DatabaseDriver = "postgres"
	settings.DatabaseURL = "postgresql://runloom:runloom@runloom-database:5432/runloom"
	require.NoError(t, ValidateKubernetes("local", "1", "http://runloom-keycloak:8080/auth", settings, "runloom"))
}

func TestAuthenticatedServiceDatabases(t *testing.T) {
	for _, tc := range []struct{ driver, uri string }{
		{"mongodb", "mongodb://runloom_auth:example@mongodb:27017/runloom_auth?replicaSet=runloom&authSource=runloom_auth"},
		{"postgres", "postgres://runloom_auth:example@postgres:5432/runloom_auth?sslmode=disable"},
	} {
		s := config.Settings{DatabaseDriver: tc.driver, DatabaseURL: tc.uri}
		require.NoError(t, Validate("local", "1", "http://keycloak:8080/auth", s))
		for _, extra := range []string{"&host=remote", "&authSource=admin", "&sslmode=require", "&replicaSet=other"} {
			s.DatabaseURL = tc.uri + extra
			require.Error(t, Validate("local", "1", "http://keycloak:8080/auth", s))
		}
	}
}

func testDirectory(t *testing.T) *access.Directory {
	t.Helper()
	r, err := access.Open(context.Background(), "file", "", "", t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, r.Close(context.Background())) })
	return &access.Directory{Repo: r}
}

func TestDirectoryIdempotencyAndConcurrency(t *testing.T) {
	ctx := context.Background()
	d := testDirectory(t)
	u := access.User{ID: "local-subject", Email: Email, Name: "Runloom Local Admin"}
	other := access.User{ID: "other-subject", Email: "other@example.test", Name: "Other"}
	org, err := d.CreateOrg(ctx, other, "Runloom")
	require.NoError(t, err)
	require.NoError(t, EnsureDirectory(ctx, d, u))
	s, _, err := d.Repo.Read(ctx)
	require.NoError(t, err)
	before, err := json.Marshal(s)
	require.NoError(t, err)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); require.NoError(t, EnsureDirectory(ctx, d, u)) }()
	}
	wg.Wait()
	s, _, err = d.Repo.Read(ctx)
	require.NoError(t, err)
	after, err := json.Marshal(s)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after))
	require.Len(t, s.Orgs, 1)
	require.Len(t, s.Orgs[org].Teams, 1)
	require.Equal(t, "owner", access.OrgRole(s, org, u.ID))
	require.Equal(t, "owner", access.OrgRole(s, org, other.ID))
	for id := range s.Orgs[org].Teams {
		_, role := access.TeamRole(s, id, u.ID)
		require.Equal(t, "admin", role)
	}
	require.NotContains(t, string(after), Password)
}

func TestDirectoryRejectsAmbiguityAndRebinding(t *testing.T) {
	ctx := context.Background()
	u := access.User{ID: "subject", Email: Email}
	t.Run("duplicate organization", func(t *testing.T) {
		d := testDirectory(t)
		_, err := d.CreateOrg(ctx, u, "Runloom")
		require.NoError(t, err)
		_, err = d.CreateOrg(ctx, access.User{ID: "other", Email: "other@example.test"}, "Runloom")
		require.NoError(t, err)
		require.ErrorContains(t, EnsureDirectory(ctx, d, u), "multiple Runloom")
	})
	t.Run("different subject", func(t *testing.T) {
		d := testDirectory(t)
		require.NoError(t, EnsureDirectory(ctx, d, u))
		u.ID = "different"
		require.ErrorContains(t, EnsureDirectory(ctx, d, u), "different identity")
	})
	t.Run("duplicate team", func(t *testing.T) {
		d := testDirectory(t)
		require.NoError(t, EnsureDirectory(ctx, d, u))
		s, _, err := d.Repo.Read(ctx)
		require.NoError(t, err)
		for id := range s.Orgs {
			_, err = d.CreateTeam(ctx, u, id, "General")
			require.NoError(t, err)
		}
		require.ErrorContains(t, EnsureDirectory(ctx, d, u), "multiple General")
	})
}

func TestIdentityCreatePreserveAndReset(t *testing.T) {
	t.Run("admin", func(t *testing.T) {
		testIdentityCreatePreserveAndReset(t, Email, Password, EnsureIdentity)
	})
	t.Run("member", func(t *testing.T) {
		testIdentityCreatePreserveAndReset(t, MemberEmail, MemberPassword, EnsureMemberIdentity)
	})
}

func testIdentityCreatePreserveAndReset(t *testing.T, email, password string, ensure func(context.Context, string, bool) (access.User, bool, error)) {
	t.Helper()
	var users []keycloakUser
	creates, resets, logouts, updates := 0, 0, 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/realms/master/protocol/openid-connect/token" {
			require.NoError(t, r.ParseForm())
			require.Equal(t, "admin-cli", r.Form.Get("client_id"))
			json.NewEncoder(w).Encode(map[string]string{"access_token": "synthetic-admin-token"})
			return
		}
		require.Equal(t, "Bearer synthetic-admin-token", r.Header.Get("Authorization"))
		switch {
		case r.Method == "GET":
			require.Equal(t, "true", r.URL.Query().Get("exact"))
			require.Equal(t, email, r.URL.Query().Get("email"))
			json.NewEncoder(w).Encode(users)
		case r.URL.Path == "/auth/admin/realms/runloom/users" && r.Method == "POST":
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.Equal(t, email, body["email"])
			require.Equal(t, true, body["emailVerified"])
			credentials := body["credentials"].([]any)[0].(map[string]any)
			require.Equal(t, password, credentials["value"])
			require.Equal(t, false, credentials["temporary"])
			creates++
			users = []keycloakUser{{ID: "subject", Username: email, Email: email, Enabled: true, Verified: true}}
			w.WriteHeader(201)
		case strings.HasSuffix(r.URL.Path, "/reset-password"):
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.Equal(t, password, body["value"])
			resets++
			w.WriteHeader(204)
		case strings.HasSuffix(r.URL.Path, "/logout"):
			logouts++
			w.WriteHeader(204)
		case r.Method == "PUT":
			updates++
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected method/path")
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	ctx := context.Background()
	u, created, err := ensure(ctx, server.URL+"/auth", false)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, "subject", u.ID)
	_, created, err = ensure(ctx, server.URL+"/auth", false)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, 1, creates)
	require.Zero(t, resets)
	require.Zero(t, updates)
	require.Zero(t, logouts)
	_, _, err = ensure(ctx, server.URL+"/auth", true)
	require.NoError(t, err)
	require.Equal(t, 1, resets)
	require.Equal(t, 1, updates)
	require.Equal(t, 1, logouts)
	users[0].Enabled = false
	_, _, err = ensure(ctx, server.URL+"/auth", false)
	require.Error(t, err)
	require.Equal(t, 1, resets)
	users = append(users, users[0])
	_, _, err = ensure(ctx, server.URL+"/auth", true)
	require.ErrorContains(t, err, "ambiguous")
	require.Equal(t, 1, resets)
}

func TestIdentityDoesNotLeakErrorsOrFollowRedirects(t *testing.T) {
	for _, status := range []int{302, 401, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "http://example.invalid/token")
				w.WriteHeader(status)
				w.Write([]byte("sensitive-provider-response"))
			}))
			defer server.Close()
			_, _, err := EnsureIdentity(context.Background(), server.URL+"/auth", false)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "sensitive-provider-response")
		})
	}
}
