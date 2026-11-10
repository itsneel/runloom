package api

import (
	"encoding/json"
	"github.com/example/runloom/workflows/internal/authoring"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthoringRestoreChecksAppliedDraftUnderLease(t *testing.T) {
	fs, err := store.New(t.TempDir())
	require.NoError(t, err)
	b := &leaseAuthoringBackend{Backend: fs}
	s := New(b, nil, "test")
	defer s.Close()
	call := func(method, path string, body any, token string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
		r.Header.Set("X-Runloom-Actor", "alice")
		r.Header.Set(editTokenHeader, token)
		r.Header.Set("X-Runloom-Authoring-Context", "full")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	d := model.Definition{SchemaVersion: "1.0", ID: "root", Name: "After Lumi", Kind: "main", Steps: []model.Step{{ID: "done", Type: "end"}}}
	w := call("POST", "/api/v1/studio/definitions", d, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	token := uuid.NewString()
	w = call("POST", "/api/v1/studio/definitions/root/edit-lock", nil, token)
	require.Equal(t, 200, w.Code)
	path := "/api/v1/studio/definitions/root/authoring"
	w = call("GET", path, nil, "")
	var graph authoring.Context
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &graph))
	zero := 0
	saved, err := b.Get("root", &zero)
	require.NoError(t, err)
	before := saved
	before.Name = "Before Lumi"
	body := map[string]any{"request_id": uuid.NewString(), "approved": true, "revision": graph.Revision, "expected_parent_hash": strings.Repeat("f", 64), "proposal": map[string]any{"operation": "update", "definition": before}}
	w = call("POST", path, body, token)
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Zero(t, b.writes)
	body["expected_parent_hash"] = authoring.Fingerprint(saved)
	w = call("POST", path, body, "")
	require.Equal(t, 423, w.Code, w.Body.String())
	require.Zero(t, b.writes)
	w = call("POST", path, body, token)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 1, b.writes)
	delete(body, "expected_parent_hash")
	body["expected_definition"] = saved
	w = call("POST", path, body, token)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 2, b.writes)
	body["restore"] = true
	incomplete := before
	incomplete.Steps = []model.Step{{ID: "unfinished-http", Type: "http"}}
	body["proposal"] = map[string]any{"operation": "update", "definition": incomplete}
	w = call("POST", path, body, token)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 3, b.writes)
	wrong := saved
	wrong.Name = "Newer manual edit"
	body["expected_definition"] = wrong
	w = call("POST", path, body, token)
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Equal(t, 3, b.writes)
}
