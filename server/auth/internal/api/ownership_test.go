package api

import (
	"context"
	"errors"
	"github.com/example/runloom/auth/internal/access"
	"github.com/example/runloom/auth/internal/model"
	"github.com/example/runloom/auth/internal/store"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strings"
	"testing"
)

type verifierStub struct{}

func (verifierStub) Verify(context.Context, string) (access.User, error) {
	return access.User{}, errors.New("unauthorized")
}

type workflowReferences struct{}

func (workflowReferences) Get(string, *int) (model.Definition, error) { return model.Definition{}, nil }
func (workflowReferences) Close(context.Context) error                { return nil }

func TestAuthOwnsOnlyIdentityRoutes(t *testing.T) {
	repo, err := access.Open(t.Context(), "file", "", "", t.TempDir())
	require.NoError(t, err)
	defer repo.Close(context.Background())
	g := NewServiceGateway(&access.Directory{Repo: repo}, verifierStub{}, access.Config{ClientID: "studio"}, func(context.Context, string) (store.Backend, error) { return workflowReferences{}, nil })
	defer g.Close()
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/api/v1/auth/config", 200}, {"/api/v1/studio/me", 401}, {"/api/v1/studio/definitions", 404}, {"/api/v1/studio/prompts", 404}, {"/api/v1/runs", 404},
	} {
		t.Run(tc.path, func(t *testing.T) {
			r := httptest.NewRequest("GET", tc.path, strings.NewReader(""))
			w := httptest.NewRecorder()
			g.ServeHTTP(w, r)
			require.Equal(t, tc.status, w.Code)
		})
	}
}
