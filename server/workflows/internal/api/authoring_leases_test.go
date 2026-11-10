package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/runloom/workflows/internal/store"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Only transaction entry is stubbed: real catalog, graph and lease checks run.
// Atomic persistence is covered by the PostgreSQL/MongoDB integration tests.
type leaseAuthoringBackend struct {
	store.Backend
	writes int
}

func (*leaseAuthoringBackend) AuthoringReceipt(context.Context, string) (store.AuthoringReceipt, error) {
	return store.AuthoringReceipt{}, store.ErrNotFound
}
func (b *leaseAuthoringBackend) CommitAuthoring(_ context.Context, batch store.AuthoringBatch) (store.AuthoringReceipt, error) {
	b.writes++
	return store.AuthoringReceipt{RequestID: batch.RequestID, ParentID: batch.Parent.ID, DraftOnly: true}, nil
}

func TestAuthoringUsesRootLeaseWithoutLeakingItToChildren(t *testing.T) {
	fs, err := store.New(t.TempDir())
	require.NoError(t, err)
	b := &leaseAuthoringBackend{Backend: fs}
	s := New(b, nil, "test")
	defer s.Close()
	call := func(method, path, body, actor, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("X-Runloom-Actor", actor)
		r.Header.Set(editTokenHeader, token)
		r.Header.Set("X-Runloom-Authoring-Context", "full")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	child := `{"schema_version":"1.0","id":"child","name":"Child","kind":"child","steps":[{"id":"done","type":"end"}]}`
	root := `{"schema_version":"1.0","id":"root","name":"Root","kind":"main","steps":[{"id":"call","type":"subworkflow","definition_id":"child","version":0},{"id":"done","type":"end"}]}`
	for _, d := range []string{child, root} {
		w := call("POST", "/api/v1/studio/definitions", d, "alice", "")
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	token := uuid.NewString()
	w := call("POST", "/api/v1/studio/definitions/root/edit-lock", "", "alice", token)
	require.Equal(t, 200, w.Code, w.Body.String())
	path := "/api/v1/studio/definitions/root/authoring"
	w = call("GET", path, "", "alice", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var graph struct {
		Revision string `json:"revision"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &graph))
	body := func() string {
		return `{"request_id":"` + uuid.NewString() + `","approved":true,"revision":"` + graph.Revision + `","proposal":{"operation":"update","definition":` + root + `}}`
	}
	for _, tc := range []struct{ actor, token string }{{"alice", ""}, {"alice", uuid.NewString()}, {"bob", token}} {
		w = call("POST", path, body(), tc.actor, tc.token)
		require.Equal(t, 423, w.Code, w.Body.String())
		require.NotContains(t, w.Body.String(), token)
	}
	w = call("POST", path, body(), "alice", token)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 1, b.writes)
	lock, err := b.EditLock(context.Background(), "root")
	require.NoError(t, err)
	require.True(t, lock.Locked)
	require.False(t, lock.Writing)
	childToken := uuid.NewString()
	w = call("POST", "/api/v1/studio/definitions/child/edit-lock", "", "alice", childToken)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = call("POST", path, body(), "alice", token)
	require.Equal(t, 423, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"workflow_id":"child"`)
	require.Equal(t, 1, b.writes)
	w = call("DELETE", "/api/v1/studio/definitions/child/edit-lock", "", "alice", childToken)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = call("DELETE", "/api/v1/studio/definitions/root/edit-lock", "", "alice", token)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = call("POST", path, body(), "alice", token)
	require.Equal(t, 423, w.Code, w.Body.String())
	require.Equal(t, 1, b.writes)
}
