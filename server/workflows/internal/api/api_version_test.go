package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/runloom/workflows/internal/access"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/stretchr/testify/require"
)

func TestAPIVersionRouting(t *testing.T) {
	backend, err := store.New(t.TempDir())
	require.NoError(t, err)
	base := New(backend, healthClient{}, "queue")
	g := &Gateway{Base: base, Config: access.Config{ClientID: "studio-client"}, Verifier: testVerifier{}}
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/healthz", 200},
		{"/api/v1/health", 200},
		{"/api/v1/auth/config", 200},
		{"/api/v1/studio/me", 401},
		{"/api/v1/studio/runs", 401},
		{"/api/v1/openapi.json", 200},
		{"/api/v1/runs", 503}, // Consumer interface, explicitly not configured here.
		{"/api/health", 404},
		{"/api/auth/config", 404},
		{"/api/me", 404},
		{"/api/runs", 404},
		{"/api/orgs", 404},
		{"/api/studio/code/test", 404},
		{"/api/v2/health", 404},
		{"/api/v2/studio/runs", 404},
		{"/api/v10/runs", 404},
	} {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			g.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			require.Equal(t, tc.status, w.Code, w.Body.String())
		})
	}
	for _, path := range []string{"/api/runs", "/api/callbacks/run/step", "/api/v2/studio/runs"} {
		w := httptest.NewRecorder()
		g.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
		require.Equal(t, http.StatusNotFound, w.Code)
	}
}
