package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
)

type childTestClient struct {
	documentedClient
	workflowType any
	starts       int
}

func TestStudioDraftSnapshotsWithoutPublishing(t *testing.T) {
	for _, kind := range []string{"main", "child"} {
		t.Run(kind, func(t *testing.T) {
			b, err := store.New(t.TempDir())
			require.NoError(t, err)
			value := model.Definition{SchemaVersion: "1.0", Kind: kind, ID: "draft", Name: "Saved draft", InputSchema: map[string]any{"type": "object", "required": []any{"name"}}, Steps: []model.Step{{ID: "echo", Type: "activity", Activity: "runloom.echo"}}}
			_, err = b.SaveDraft(value)
			require.NoError(t, err)
			c := &childTestClient{}
			s := New(b, c, "test")
			defer s.Close()
			call := func(path string, body any, status int) {
				t.Helper()
				raw, err := json.Marshal(body)
				require.NoError(t, err)
				r := httptest.NewRequest("POST", path, strings.NewReader(string(raw)))
				r.Header.Set("X-Runloom-Team", "team")
				r.Header.Set("X-Runloom-Actor", "editor")
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, r)
				require.Equal(t, status, w.Code, w.Body.String())
			}
			value.Name, value.Version = "Unsaved changes", 9
			body := map[string]any{"definition": value, "input": map[string]any{"name": "Avery"}}
			call("/api/v1/studio/definitions/draft/test", body, 201)
			require.Equal(t, "RunloomDraftTestWorkflow", c.workflowType)
			require.Equal(t, "Unsaved changes", c.input.Definition.Name)
			require.Zero(t, c.input.Definition.Version)
			require.True(t, c.input.IsTest)
			require.Equal(t, "draft_test", c.input.Source)
			record, err := b.GetRun(c.id)
			require.NoError(t, err)
			require.Equal(t, "draft_test", record.TriggerType)
			zero := 0
			saved, err := b.Get("draft", &zero)
			require.NoError(t, err)
			require.Equal(t, "Saved draft", saved.Name)
			_, err = b.Get("draft", nil)
			require.Error(t, err, "testing must not publish")
			call("/api/v1/studio/runs", map[string]any{"definition_id": "draft", "input": map[string]any{"name": "Avery"}}, 404)
			call("/api/v1/studio/definitions/other/test", body, 422)
			body["input"] = map[string]any{}
			call("/api/v1/studio/definitions/draft/test", body, 422)
			body["input"] = map[string]any{"name": "Avery"}
			value.Steps = []model.Step{{ID: "nested", Type: "subworkflow", DefinitionID: "missing", Version: 1}}
			body["definition"] = value
			call("/api/v1/studio/definitions/draft/test", body, 422)
			value.ID = "missing"
			body["definition"] = value
			call("/api/v1/studio/definitions/missing/test", body, 404)
			require.Equal(t, 1, c.starts)
		})
	}
}

func (c *childTestClient) ExecuteWorkflow(ctx context.Context, options client.StartWorkflowOptions, typ any, args ...any) (client.WorkflowRun, error) {
	c.workflowType = typ
	c.starts++
	return c.documentedClient.ExecuteWorkflow(ctx, options, typ, args...)
}

func TestStudioChildTestContract(t *testing.T) {
	b, err := store.New(t.TempDir())
	require.NoError(t, err)
	value := model.Definition{SchemaVersion: "1.0", Kind: "child", ID: "child", Name: "Child", InputSchema: map[string]any{"type": "object", "required": []any{"name"}}, Steps: []model.Step{{ID: "echo", Type: "activity", Activity: "runloom.echo"}}}
	_, err = b.SaveDraft(value)
	require.NoError(t, err)
	_, err = b.Publish(value.ID)
	require.NoError(t, err)
	value.ID, value.Kind = "main", "main"
	_, err = b.SaveDraft(value)
	require.NoError(t, err)
	_, err = b.Publish(value.ID)
	require.NoError(t, err)
	c := &childTestClient{}
	s := New(b, c, "test")
	defer s.Close()
	call := func(path, body string, status int) {
		t.Helper()
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("X-Runloom-Team", "team")
		r.Header.Set("X-Runloom-Actor", "editor")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		require.Equal(t, status, w.Code, w.Body.String())
	}
	call("/api/v1/studio/definitions/child/test", `{"version":1,"input":{"name":"Avery"}}`, 201)
	require.Equal(t, "RunloomChildTestWorkflow", c.workflowType)
	require.True(t, c.input.IsTest)
	require.Equal(t, "child_test", c.input.Source)
	require.Equal(t, "child", c.input.Definition.Kind)
	record, err := b.GetRun(c.id)
	require.NoError(t, err)
	require.Equal(t, "child_test", record.TriggerType)
	events, err := b.ListAudit(c.id, "", 100)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "editor", events[0].Actor)
	for _, body := range []string{`{"version":0}`, `{"input":{"name":"Avery"}}`, `{"version":1,"input":{}}`, `{"version":1,"is_test":false,"input":{"name":"Avery"}}`} {
		call("/api/v1/studio/definitions/child/test", body, 422)
	}
	call("/api/v1/studio/definitions/main/test", `{"version":1,"input":{"name":"Avery"}}`, 422)
	call("/api/v1/studio/definitions/missing/test", `{"version":1}`, 404)
	call("/api/v1/studio/runs", `{"definition_id":"child","version":1,"input":{"name":"Avery"},"is_test":true}`, 422)
	call("/api/v1/studio/runs", `{"definition_id":"child","version":1,"input":{"name":"Avery"}}`, 422)
	require.Equal(t, 1, c.starts, "rejected requests must not start Temporal workflows")
}
