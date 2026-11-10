package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/runloom/workflows/internal/activities"
	"github.com/example/runloom/workflows/internal/engine"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/example/runloom/workflows/internal/usage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

// Opt-in local Temporal test: isolated team/queue and loopback fake LLM only.
// No paid API, provider account or production worker configuration is touched.
func TestLLMUsageLive(t *testing.T) {
	address := os.Getenv("RUNLOOM_TEST_TEMPORAL_ADDRESS")
	if address == "" {
		t.Skip("set RUNLOOM_TEST_TEMPORAL_ADDRESS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c, err := client.Dial(client.Options{HostPort: address})
	require.NoError(t, err)
	defer c.Close()
	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		w.Header().Set("X-Request-ID", "synthetic-request")
		if n == 2 {
			w.WriteHeader(503)
		} // Child's first attempt is billable but fails.
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "test-model", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": `{"content":"synthetic output","hitl_required":false,"hitl_reason":""}`}}}, "usage": map[string]any{"prompt_tokens": 1000, "completion_tokens": 500}})
	}))
	defer provider.Close()
	t.Setenv("RUNLOOM_LLM_API_KEY_FILE", "")
	t.Setenv("RUNLOOM_LLM_API_KEY", "synthetic-local-only")
	t.Setenv("RUNLOOM_LLM_BASE_URL", provider.URL)
	team := uuid.NewString()
	queue := "runloom-usage-test-" + team
	data := t.TempDir()
	openTeam := func(ctx context.Context, id string) (store.Backend, error) {
		return store.OpenTeam(ctx, os.Getenv("RUNLOOM_TEST_DATABASE_DRIVER"), os.Getenv("RUNLOOM_TEST_DATABASE_URL"), "runloom_usage_tests", data, id)
	}
	b, err := openTeam(ctx, team)
	require.NoError(t, err)
	defer b.Close(context.Background())
	prices, err := usage.Load("../../testdata/deploy/llm-pricing.example.json")
	require.NoError(t, err)
	meter := activities.MeteredLLM{OpenTeam: func(ctx context.Context, id string) (activities.MeteringStore, error) { return openTeam(ctx, id) }, Prices: prices, Provider: "local-test", Ancestors: activities.LLMAncestors(c)}
	recorder := activities.RunRecorder{OpenTeam: func(ctx context.Context, id string) (activities.RecordingStore, error) { return openTeam(ctx, id) }}
	w := worker.New(c, queue, worker.Options{})
	w.RegisterWorkflow(engine.RunloomWorkflow)
	w.RegisterActivityWithOptions(meter.Call, activity.RegisterOptions{Name: "runloom.llm"})
	w.RegisterActivityWithOptions(recorder.Record, activity.RegisterOptions{Name: "runloom.internal.record_run"})
	require.NoError(t, w.Start())
	defer w.Stop()
	s := New(b, c, queue)
	defer s.Close()
	call := func(method, path string, body any, status int) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		r := httptest.NewRequest(method, path, strings.NewReader(string(raw))).WithContext(ctx)
		r.Header.Set("X-Runloom-Team", team)
		r.Header.Set("X-Runloom-Actor", "usage-test")
		response := httptest.NewRecorder()
		s.Handler().ServeHTTP(response, r)
		require.Equal(t, status, response.Code, response.Body.String())
		return response
	}
	llm := model.Step{ID: "ask", Type: "llm", Model: "test-model", Prompt: "synthetic prompt", Retry: &model.RetryPolicy{MaximumAttempts: 2, InitialInterval: 1, BackoffCoefficient: 2, MaximumInterval: 2}}
	child := model.Definition{SchemaVersion: "1.0", ID: "usage-child", Name: "Usage child", Kind: "child", Steps: []model.Step{llm}, Output: "${steps.ask.output.content}"}
	parent := model.Definition{SchemaVersion: "1.0", ID: "usage-parent", Name: "Usage parent", Steps: []model.Step{llm, {ID: "child", Type: "subworkflow", DefinitionID: child.ID, Version: 1, With: map[string]any{}}}}
	for _, d := range []model.Definition{child, parent} {
		call("POST", "/api/v1/studio/definitions", d, 200)
		call("POST", "/api/v1/studio/definitions/"+d.ID+"/publish", map[string]any{}, 200)
	}
	started := call("POST", "/api/v1/studio/runs", map[string]any{"definition_id": parent.ID, "version": 1, "input": map[string]any{}}, 201)
	var run struct {
		ID    string `json:"workflow_id"`
		RunID string `json:"run_id"`
	}
	require.NoError(t, json.Unmarshal(started.Body.Bytes(), &run))
	var result model.RunResult
	require.NoError(t, c.GetWorkflow(ctx, run.ID, run.RunID).Get(ctx, &result))
	require.Equal(t, int32(3), requests.Load())
	call("GET", "/api/v1/studio/runs/"+run.ID, nil, 200)
	response := call("GET", "/api/v1/studio/runs/"+run.ID+"/llm-usage", nil, 200)
	var page struct {
		Summary usage.Summary `json:"summary"`
		Entries []usage.Entry `json:"entries"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
	require.Len(t, page.Entries, 3)
	require.Equal(t, "0.006000000000", page.Summary.EstimatedUSD)
	require.Equal(t, "0.002000000000", page.Summary.DirectUSD)
	require.Equal(t, "0.004000000000", page.Summary.ChildUSD)
	require.True(t, page.Summary.Complete)
	failed, retried := 0, 0
	for _, e := range page.Entries {
		require.Equal(t, "ask", e.StepID)
		require.Equal(t, "synthetic-test-v1", e.Pricing.Version)
		if e.Status == "failed" {
			failed++
		}
		if e.Attempt == 2 {
			retried++
		}
	}
	require.Equal(t, 1, failed)
	require.Equal(t, 1, retried)
	// Replay consumes recorded commands/results without another provider request.
	history := &historypb.History{}
	iterator := c.GetWorkflowHistory(ctx, run.ID, run.RunID, false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	for iterator.HasNext() {
		event, err := iterator.Next()
		require.NoError(t, err)
		history.Events = append(history.Events, event)
	}
	replayer := worker.NewWorkflowReplayer()
	replayer.RegisterWorkflow(engine.RunloomWorkflow)
	require.NoError(t, replayer.ReplayWorkflowHistory(nil, history))
	require.Equal(t, int32(3), requests.Load())
}
