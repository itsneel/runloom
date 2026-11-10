package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/runloom/workflows/internal/store"
	"github.com/example/runloom/workflows/internal/usage"
	"github.com/stretchr/testify/require"
)

func TestRunUsagePaginationRollupAndMissingData(t *testing.T) {
	b, err := store.New(t.TempDir())
	require.NoError(t, err)
	s := New(b, nil, "queue")
	defer s.Close()
	run, err := b.RecordRunStart(store.RunRecord{WorkflowID: "parent", TemporalRunID: "run-1", TemporalStatus: "completed"})
	require.NoError(t, err)
	key := usage.RunKey(run.WorkflowID, run.TemporalRunID)
	for i, id := range []string{"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002", "00000000-0000-4000-8000-000000000003"} {
		row := usage.Entry{ID: id, WorkflowID: "parent", TemporalRunID: "run-1", Ancestors: []string{key}, StepID: "llm", Model: "test-model", Status: "pending", Accounting: "pending", StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}
		if i == 1 {
			row.WorkflowID = "child"
			row.TemporalRunID = "run-child"
		}
		_, err = b.BeginUsage(context.Background(), row)
		require.NoError(t, err)
		if i != 2 {
			row.Status = "succeeded"
			row.Accounting = "estimated"
			row.CostUSD = "0.100000000000"
			require.NoError(t, b.FinishUsage(context.Background(), row))
		}
	}
	call := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.SetPathValue("id", "parent")
		w := httptest.NewRecorder()
		s.runLLMUsage(w, r)
		return w
	}
	type page struct {
		Summary usage.Summary `json:"summary"`
		Entries []usage.Entry `json:"entries"`
		Next    string        `json:"next_cursor"`
	}
	w := call("/api/v1/studio/runs/parent/llm-usage?limit=1")
	require.Equal(t, 200, w.Code, w.Body.String())
	var first page
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &first))
	require.Len(t, first.Entries, 1)
	require.NotEmpty(t, first.Next)
	require.Equal(t, "0.200000000000", first.Summary.EstimatedUSD)
	require.Equal(t, "0.100000000000", first.Summary.ChildUSD)
	require.Equal(t, 1, first.Summary.Unknown)
	require.False(t, first.Summary.Complete)
	w = call("/api/v1/studio/runs/parent/llm-usage?limit=1&after=" + first.Next)
	var second page
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &second))
	require.Equal(t, "child", second.Entries[0].WorkflowID)
	w = call("/api/v1/studio/runs/parent/llm-usage?model=missing")
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &second))
	require.Empty(t, second.Entries)
	require.Equal(t, 3, second.Summary.Requests)
	for _, query := range []string{"limit=101", "limit=0"} {
		require.Equal(t, 422, call("/api/v1/studio/runs/parent/llm-usage?"+query).Code)
	}
	require.Equal(t, 400, call("/api/v1/studio/runs/parent/llm-usage?after=bad").Code)
	missing, err := usageSummary(context.Background(), b, store.RunRecord{WorkflowID: "old", TemporalRunID: "old-run"})
	require.NoError(t, err)
	require.False(t, missing.Complete)
	require.Zero(t, missing.Requests)
}
