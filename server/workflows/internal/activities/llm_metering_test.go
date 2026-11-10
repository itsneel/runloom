package activities

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/example/runloom/workflows/internal/usage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/workflow"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type usageFailure struct {
	store.Backend
	beginFail, finishFail bool
}

func (b usageFailure) Close(context.Context) error { return nil }
func (b usageFailure) BeginUsage(ctx context.Context, e usage.Entry) (bool, error) {
	if b.beginFail {
		return false, errors.New("storage failed")
	}
	return b.Backend.BeginUsage(ctx, e)
}
func (b usageFailure) FinishUsage(ctx context.Context, e usage.Entry) error {
	if b.finishFail {
		return errors.New("storage failed")
	}
	return b.Backend.FinishUsage(ctx, e)
}

func TestMeteredLLMRequestsAndFailures(t *testing.T) {
	ctx := context.Background()
	b, err := store.New(t.TempDir())
	require.NoError(t, err)
	team := uuid.NewString()
	id := team + "-run"
	run := uuid.NewString()
	_, err = b.RecordRunStart(store.RunRecord{WorkflowID: id, TemporalRunID: run, DefinitionID: "workflow", DefinitionVersion: 1, TemporalStatus: "running"})
	require.NoError(t, err)
	info := activity.Info{WorkflowNamespace: "default", WorkflowExecution: workflow.Execution{ID: id, RunID: run}, ActivityID: "1", Attempt: 1}
	calls := 0
	validContent := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("X-Request-ID", "request-1")
		choices := []any{}
		if validContent {
			choices = []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": `{"content":"secret response","hitl_required":false,"hitl_reason":""}`}}}
		}
		json.NewEncoder(w).Encode(map[string]any{"model": "test-model", "choices": choices, "usage": map[string]any{"prompt_tokens": 1000, "completion_tokens": 500}})
	}))
	defer server.Close()
	t.Setenv("RUNLOOM_LLM_API_KEY", "secret-key")
	t.Setenv("RUNLOOM_LLM_BASE_URL", server.URL)
	failure := usageFailure{Backend: b}
	meter := MeteredLLM{Provider: "test", OpenTeam: func(context.Context, string) (MeteringStore, error) { return failure, nil }, Ancestors: func(context.Context, string, string) ([]string, error) { return []string{usage.RunKey(id, run)}, nil }, Prices: usage.Catalogue{Rates: []usage.Rate{{Provider: "test", Model: "test-model", Currency: "USD", Version: "v1", EffectiveAt: "2020-01-01T00:00:00Z", Input: "1", Output: "2"}}}}
	payload := map[string]any{"model": "test-model", "system_prompt": "", "prompt": "secret prompt", "max_tokens": 1000, "_runloom_step_id": "llm-node"}
	call := func() (map[string]any, error) {
		return callLLM(ctx, payload, func(ctx context.Context, model string) (func(llmObservation) bool, error) {
			return meter.begin(ctx, payload, model, info)
		})
	}
	_, err = call()
	require.Error(t, err)
	require.Equal(t, 1, calls)
	validContent = true
	info.Attempt = 2
	result, err := call()
	require.NoError(t, err)
	require.Equal(t, true, result["usage_recorded"])
	rows, err := b.QueryUsage(ctx, usage.Filter{RunKey: usage.RunKey(id, run)})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	summary := usage.Summarize(rows, usage.RunKey(id, run), false)
	require.Equal(t, "0.004000000000", summary.EstimatedUSD)
	_, err = call()
	require.ErrorContains(t, err, "already recorded")
	require.Equal(t, 2, calls)
	info.Attempt = 3
	failure.beginFail = true
	_, err = call()
	require.Error(t, err)
	require.Equal(t, 2, calls)
	failure.beginFail = false
	failure.finishFail = true
	result, err = call()
	require.NoError(t, err)
	require.Equal(t, false, result["usage_recorded"])
	require.Equal(t, 3, calls)
	rows, err = b.QueryUsage(ctx, usage.Filter{RunKey: usage.RunKey(id, run)})
	require.NoError(t, err)
	require.Len(t, rows, 3)
	raw, _ := json.Marshal(rows)
	require.False(t, strings.Contains(string(raw), "secret"), "ledger must not contain prompts, responses or keys")
}

func TestDraftLLMUsageIsClassifiedBeforeStatusReconciliation(t *testing.T) {
	ctx := context.Background()
	b, err := store.New(t.TempDir())
	require.NoError(t, err)
	team, runID := uuid.NewString(), uuid.NewString()
	id := team + "-test"
	_, err = b.RecordRunStart(store.RunRecord{WorkflowID: id, TemporalRunID: runID, DefinitionID: "draft", TriggerType: "draft_test", TemporalStatus: "running"})
	require.NoError(t, err)
	meter := MeteredLLM{Provider: "test", OpenTeam: func(context.Context, string) (MeteringStore, error) { return usageFailure{Backend: b}, nil }, Ancestors: func(context.Context, string, string) ([]string, error) { return []string{usage.RunKey(id, runID)}, nil }}
	_, err = meter.begin(ctx, map[string]any{"_runloom_step_id": "llm"}, "test-model", activity.Info{WorkflowNamespace: "default", WorkflowExecution: workflow.Execution{ID: id, RunID: runID}, ActivityID: "1", Attempt: 1})
	require.NoError(t, err)
	rows, err := b.QueryUsage(ctx, usage.Filter{RunKey: usage.RunKey(id, runID)})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.True(t, rows[0].IsTest)
	require.Zero(t, rows[0].DefinitionVersion)
}
