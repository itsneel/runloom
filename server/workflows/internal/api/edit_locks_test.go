package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/runloom/workflows/internal/store"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestWorkflowEditLockEnforcesAllMutationRoutes(t *testing.T) {
	b, err := store.New(t.TempDir())
	require.NoError(t, err)
	s := New(b, nil, "test")
	defer s.Close()
	call := func(method, path, body, actor, token string, code int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("X-Runloom-Actor", actor)
		r.Header.Set(editTokenHeader, token)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		require.Equal(t, code, w.Code, w.Body.String())
		return w
	}
	draft := `{"schema_version":"1.0","id":"edit-test","name":"Edit test","trigger":{"type":"manual"},"steps":[{"id":"echo","type":"activity","activity":"runloom.echo"}]}`
	call("POST", "/api/v1/studio/definitions", draft, "alice", "", 200)
	commits, err := b.ListCommits("edit-test", 10)
	require.NoError(t, err)
	token := uuid.NewString()
	lockURL := "/api/v1/studio/definitions/edit-test/edit-lock"
	call("POST", lockURL, "", "alice", token, 200)
	status := call("GET", lockURL, "", "bob", "", 200)
	require.NotContains(t, status.Body.String(), token)
	call("POST", lockURL, "", "bob", uuid.NewString(), 423)
	call("DELETE", lockURL, "", "bob", token, 423)
	for _, owner := range []string{"alice", "bob"} {
		call("POST", "/api/v1/studio/definitions", draft, owner, "", 423)
		call("POST", "/api/v1/studio/definitions/edit-test/publish", "{}", owner, "", 423)
		call("POST", "/api/v1/studio/definitions/edit-test/commits/"+commits[0].CommitID+"/restore", "{}", owner, "", 423)
		call("POST", "/api/v1/studio/definitions/edit-test/project", `{"project_id":"default"}`, owner, "", 423)
	}
	call("POST", "/api/v1/studio/definitions", draft, "bob", token, 423)
	call("GET", "/api/v1/studio/definitions/edit-test?version=0", "", "bob", "", 200)
	call("POST", "/api/v1/studio/definitions", draft, "alice", token, 200)
	call("POST", "/api/v1/studio/definitions/edit-test/publish", "{}", "alice", token, 200)
	call("POST", "/api/v1/studio/definitions/edit-test/commits/"+commits[0].CommitID+"/restore", "{}", "alice", token, 200)
	call("POST", "/api/v1/studio/definitions/edit-test/project", `{"project_id":"default"}`, "alice", token, 200)
	call("DELETE", lockURL, "", "alice", token, 200)
	call("POST", "/api/v1/studio/definitions", draft, "alice", token, 423)
	call("POST", "/api/v1/studio/definitions", draft, "bob", "", 200)
	status = call("GET", lockURL, "", "bob", "", 200)
	var lock store.EditLockStatus
	require.NoError(t, json.Unmarshal(status.Body.Bytes(), &lock))
	require.False(t, lock.Locked)
	call("POST", lockURL, "", "alice", "not-a-token", 423)
}
