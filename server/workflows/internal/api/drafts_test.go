package api

import (
	"encoding/json"
	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/prompts"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidationReturnsAllNodeIssues(t *testing.T) {
	b, err := store.New(t.TempDir())
	require.NoError(t, err)
	s := New(b, &childTestClient{}, "test")
	defer s.Close()
	d := model.Definition{SchemaVersion: "1.0", ID: "diagnostics", Name: "Diagnostics", Steps: []model.Step{
		{ID: "http", Type: "http"},
		{ID: "ask", Type: "llm", Model: "default", PromptRef: &prompts.Reference{ID: "missing", Version: 1}},
		{ID: "child", Type: "subworkflow", DefinitionID: "missing", Version: 1},
		{ID: "wait", Type: "wait", Seconds: 0},
	}}
	raw, _ := json.Marshal(d)
	r := httptest.NewRequest("POST", "/api/v1/studio/definitions/validate", strings.NewReader(string(raw)))
	r.Header.Set("X-Runloom-Team", "team")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	require.Equal(t, 422, w.Code, w.Body.String())
	var body struct {
		Error  string                 `json:"detail"`
		Issues []definition.NodeIssue `json:"issues"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.NotEmpty(t, body.Error)
	ids := map[string]bool{}
	for _, issue := range body.Issues {
		ids[issue.NodeID] = true
	}
	require.Equal(t, map[string]bool{"http": true, "ask": true, "child": true, "wait": true}, ids)
}

func TestInvalidDraftSavePublishAndTest(t *testing.T) {
	b, err := store.New(t.TempDir())
	require.NoError(t, err)
	c := &childTestClient{}
	s := New(b, c, "test")
	defer s.Close()
	call := func(path string, body any, status int) {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", path, strings.NewReader(string(raw)))
		r.Header.Set("X-Runloom-Team", "team")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		require.Equal(t, status, w.Code, w.Body.String())
	}
	d := model.Definition{SchemaVersion: "1.0", ID: "unfinished", Name: "Unfinished", Steps: []model.Step{{ID: "done", Type: "end"}}}
	call("/api/v1/studio/definitions", d, 200)
	call("/api/v1/studio/definitions/unfinished/publish", nil, 200)
	for _, steps := range [][]model.Step{
		{}, {{ID: "ask", Type: "llm", Model: "default"}},
		{{ID: "child", Type: "subworkflow", DefinitionID: "not-yet-created", Version: 1}},
		{{ID: "choose", Type: "parallel", Branches: []model.Branch{{ID: "one", Steps: []model.Step{}}}}},
		{{ID: "ask", Type: "llm", Model: "default", PromptRef: &prompts.Reference{ID: "missing", Version: 1}}},
		{{ID: "api", Type: "http"}},
	} {
		d.Steps = steps
		call("/api/v1/studio/definitions", d, 200)
		call("/api/v1/studio/definitions/validate", d, 422)
		call("/api/v1/studio/definitions/unfinished/publish", nil, 422)
		call("/api/v1/studio/definitions/unfinished/test", map[string]any{"definition": d, "input": map[string]any{}}, 422)
		latest, err := b.Get(d.ID, nil)
		require.NoError(t, err)
		require.Equal(t, 1, latest.Version)
		require.Equal(t, "done", latest.Steps[0].ID)
	}
	require.Zero(t, c.starts)
	d.Steps = []model.Step{{ID: "fixed", Type: "end"}}
	call("/api/v1/studio/definitions", d, 200)
	call("/api/v1/studio/definitions/unfinished/publish", nil, 200)
	latest, err := b.Get(d.ID, nil)
	require.NoError(t, err)
	require.Equal(t, 2, latest.Version)
	d.ID = "../escape"
	call("/api/v1/studio/definitions", d, 422)
}
