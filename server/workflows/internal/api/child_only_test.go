package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/stretchr/testify/require"
)

func TestChildOnlyStartEndpoints(t *testing.T) {
	b, err := store.New(t.TempDir())
	require.NoError(t, err)
	child := model.Definition{SchemaVersion: "1.0", Kind: "child", ID: "child", Name: "Child", Steps: []model.Step{{ID: "echo", Type: "activity", Activity: "runloom.echo"}}}
	_, err = b.SaveDraft(child)
	require.NoError(t, err)
	_, err = b.Publish(child.ID)
	require.NoError(t, err)
	// A nil Temporal client proves these requests stop before scheduling work.
	s := New(b, nil, "test")
	defer s.Close()
	for _, tc := range []struct{ path, body string }{
		{"/api/v1/studio/runs", `{"definition_id":"child","input":{}}`},
		{"/api/v1/studio/runs", `{"definition_id":"child","version":1,"workflow_id":"custom","input":{}}`},
		{"/api/v1/studio/webhooks/child/anything", `{}`},
		{"/api/v1/studio/schedules", `{"name":"Child schedule","definition_id":"child","version":1,"interval_seconds":60,"input":{}}`},
	} {
		req := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
		result := httptest.NewRecorder()
		s.Handler().ServeHTTP(result, req)
		require.Equal(t, 422, result.Code, result.Body.String())
		require.Contains(t, result.Body.String(), "only be started by a parent")
	}
	runs, err := b.ListRuns(100)
	require.NoError(t, err)
	require.Empty(t, runs)
	// The listing describes the published kind, not an unsaved/new main draft.
	child.Kind = "main"
	_, err = b.SaveDraft(child)
	require.NoError(t, err)
	req := httptest.NewRequest("GET", "/api/v1/studio/definitions", nil)
	result := httptest.NewRecorder()
	s.Handler().ServeHTTP(result, req)
	require.Equal(t, 200, result.Code)
	var summaries []store.Summary
	require.NoError(t, json.Unmarshal(result.Body.Bytes(), &summaries))
	require.Equal(t, "child", summaries[0].PublishedKind)
}
