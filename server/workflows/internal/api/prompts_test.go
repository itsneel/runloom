package api

import (
	"encoding/json"
	"github.com/example/runloom/workflows/internal/access"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/prompts"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPromptAPIAndPaidTestIdentity(t *testing.T) {
	b, err := store.New(t.TempDir())
	require.NoError(t, err)
	c := &childTestClient{}
	s := New(b, c, "test")
	defer s.Close()
	call := func(method, path string, body any, status int) []byte {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
		r.Header.Set("X-Runloom-Team", "team")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		require.Equal(t, status, w.Code, w.Body.String())
		return w.Body.Bytes()
	}
	e := prompts.Entry{ID: "summary", Name: "Summary", Template: prompts.Template{Text: "${vars.text}", Variables: []prompts.Variable{{Name: "text", Type: "string", Required: true}}}}
	raw := call("POST", "/api/v1/studio/prompts/summary/publish", e, 200)
	require.NoError(t, json.Unmarshal(raw, &e))
	require.Equal(t, 1, e.Version)
	stale := e
	stale.Revision = 0
	call("POST", "/api/v1/studio/prompts/summary/save", stale, 409)
	call("POST", "/api/v1/studio/prompts/preview", map[string]any{"template": e.Template, "inputs": map[string]any{"text": "${input.secret}"}}, 200)
	call("POST", "/api/v1/studio/prompts/preview", map[string]any{"template": e.Template, "inputs": map[string]any{"text": false}}, 422)
	d := model.Definition{SchemaVersion: "1.0", ID: "example", Name: "Example", Steps: []model.Step{{ID: "ask", Type: "llm", Model: "default", PromptRef: &prompts.Reference{ID: e.ID, Version: 1}, PromptInputs: map[string]any{"text": "hi"}}}}
	raw = call("POST", "/api/v1/studio/definitions", d, 200)
	require.NoError(t, json.Unmarshal(raw, &d))
	require.Equal(t, e.Template, *d.Steps[0].PromptTemplate)
	call("POST", "/api/v1/studio/prompts/test", map[string]any{"definition_id": "example", "template": e.Template, "inputs": map[string]any{"text": "${input.secret}"}, "model": "default"}, 201)
	require.Equal(t, "example", c.input.Definition.ID)
	require.Equal(t, "${input.secret}", c.input.Input["text"])
	require.Equal(t, "${input.text}", c.input.Definition.Steps[0].PromptInputs["text"])
	require.True(t, c.input.IsTest)
	call("POST", "/api/v1/studio/prompts/test", map[string]any{"definition_id": "missing", "template": e.Template, "inputs": map[string]any{"text": "test"}, "model": "default"}, 404)
	require.Equal(t, 1, c.starts)
}

func TestPromptGatewayIsolation(t *testing.T) {
	f := newConsumerFixture(t)
	ctx := t.Context()
	tokens := f.g.Verifier.(testVerifier)
	admin := tokens["admin"]
	viewer := access.User{ID: "viewer", Email: "viewer@example.test"}
	tokens["viewer"] = viewer
	_, err := f.directory.Bind(ctx, viewer)
	require.NoError(t, err)
	require.NoError(t, f.directory.SetMember(ctx, admin, f.org, "", viewer.Email, "member", false))
	require.NoError(t, f.directory.SetMember(ctx, admin, f.org, f.team, viewer.Email, "viewer", false))
	_, err = f.directory.Bind(ctx, viewer)
	require.NoError(t, err)
	other, err := f.directory.CreateTeam(ctx, admin, f.org, "Other")
	require.NoError(t, err)
	call := func(method, path, token, team string, body any, status int) {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-Runloom-Team", team)
		w := httptest.NewRecorder()
		f.g.ServeHTTP(w, r)
		require.Equal(t, status, w.Code, w.Body.String())
	}
	e := prompts.Entry{ID: "private", Name: "Private", Template: prompts.Template{Text: "Team only"}}
	call("POST", "/api/v1/studio/prompts/private/publish", "admin", f.team, e, 200)
	call("GET", "/api/v1/studio/prompts/private?version=1", "viewer", f.team, nil, 200)
	call("POST", "/api/v1/studio/prompts/private/publish", "viewer", f.team, e, 403)
	call("GET", "/api/v1/studio/prompts/private?version=1", "admin", other, nil, 404)
	call("GET", "/api/v1/studio/prompts", "viewer", other, nil, 403)
	account := f.account()
	call("GET", "/api/v1/studio/prompts", account.ClientID, f.team, nil, 401)
}
