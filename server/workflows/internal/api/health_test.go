package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/runloom/workflows/internal/store"
	"go.temporal.io/sdk/client"
)

type healthClient struct {
	client.Client
	err error
}

func (c healthClient) CheckHealth(context.Context, *client.CheckHealthRequest) (*client.CheckHealthResponse, error) {
	return &client.CheckHealthResponse{}, c.err
}

type healthStore struct {
	store.Backend
	ok bool
}

func (s healthStore) Health(context.Context) bool { return s.ok }
func (s healthStore) Driver() string              { return "postgres" }

func TestHealthProbes(t *testing.T) {
	for _, tc := range []struct {
		name string
		dbOK bool
		err  error
		code int
	}{
		{"healthy", true, nil, http.StatusOK},
		{"database unavailable", false, nil, http.StatusServiceUnavailable},
		{"temporal unavailable", true, errors.New("unavailable"), http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{modules: store.Bind(healthStore{ok: tc.dbOK}), temporal: healthClient{err: tc.err}, mux: http.NewServeMux()}
			s.routes()
			for path, want := range map[string]int{"/api/v1/health": tc.code, "/healthz": http.StatusOK} {
				r := httptest.NewRecorder()
				s.Handler().ServeHTTP(r, httptest.NewRequest(http.MethodGet, path, nil))
				if r.Code != want {
					t.Fatalf("%s: got %d, want %d", path, r.Code, want)
				}
			}
		})
	}
}
