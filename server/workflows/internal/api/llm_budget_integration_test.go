package api

import (
	"context"
	"encoding/json"
	"github.com/example/runloom/workflows/internal/activities"
	"github.com/example/runloom/workflows/internal/budget"
	"github.com/example/runloom/workflows/internal/definition"
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
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// Isolated queue/team and loopback fake provider; never calls a paid model.
func TestLLMBudgetLive(t *testing.T) {
	address := os.Getenv("RUNLOOM_TEST_TEMPORAL_ADDRESS")
	if address == "" {
		t.Skip("opt-in local Temporal")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Second)
	defer cancel()
	c, err := client.Dial(client.Options{HostPort: address})
	require.NoError(t, err)
	defer c.Close()
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "test-model", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": `{"content":"retained response","hitl_required":false,"hitl_reason":""}`}}}, "usage": map[string]any{"prompt_tokens": 1000, "completion_tokens": 500}})
	}))
	defer provider.Close()
	t.Setenv("RUNLOOM_LLM_API_KEY_FILE", "")
	t.Setenv("RUNLOOM_LLM_API_KEY", "synthetic-local-only")
	t.Setenv("RUNLOOM_LLM_BASE_URL", provider.URL)
	team := uuid.NewString()
	queue := "budget-test-" + team
	data := t.TempDir()
	open := func(ctx context.Context, id string) (store.Backend, error) {
		return store.OpenTeam(ctx, os.Getenv("RUNLOOM_TEST_DATABASE_DRIVER"), os.Getenv("RUNLOOM_TEST_DATABASE_URL"), "runloom_budget_tests", data, id)
	}
	b, err := open(ctx, team)
	require.NoError(t, err)
	defer b.Close(context.Background())
	p, err := b.SaveBudgetPolicy(ctx, budget.Policy{ID: uuid.NewString(), Name: "Monthly budget", Enabled: true, SoftUSD: "0", HardUSD: "0", Reason: "Synthetic verification"}, "test-admin")
	require.NoError(t, err)
	prices, err := usage.Load("../../testdata/deploy/llm-pricing.example.json")
	require.NoError(t, err)
	meter := activities.MeteredLLM{OpenTeam: func(ctx context.Context, id string) (activities.MeteringStore, error) { return open(ctx, id) }, Ancestors: activities.LLMAncestors(c), Prices: prices, Provider: "local-test"}
	recorder := activities.RunRecorder{OpenTeam: func(ctx context.Context, id string) (activities.RecordingStore, error) { return open(ctx, id) }}
	newWorker := func() worker.Worker {
		w := worker.New(c, queue, worker.Options{})
		w.RegisterWorkflow(engine.RunloomWorkflow)
		w.RegisterActivityWithOptions(meter.Call, activity.RegisterOptions{Name: "runloom.llm"})
		w.RegisterActivityWithOptions(meter.Check, activity.RegisterOptions{Name: "runloom.internal.check_llm_budget"})
		w.RegisterActivityWithOptions(recorder.Record, activity.RegisterOptions{Name: "runloom.internal.record_run"})
		return w
	}
	w := newWorker()
	require.NoError(t, w.Start())
	defer func() { w.Stop() }()
	def, err := definition.Normalize(model.Definition{SchemaVersion: "1.0", ID: "budget-live", Name: "Budget live", Steps: []model.Step{{ID: "first", Type: "llm", Model: "test-model", Prompt: "synthetic"}, {ID: "second", Type: "llm", Model: "test-model", Prompt: "synthetic"}}, Output: "${steps.first.output.content}"})
	require.NoError(t, err)
	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{ID: team + "-budget-" + uuid.NewString(), TaskQueue: queue}, engine.RunloomWorkflow, model.RunInput{Definition: def, TeamID: team, Source: "schedule", Input: map[string]any{}})
	require.NoError(t, err)
	paused := func() {
		require.Eventually(t, func() bool {
			value, e := c.QueryWorkflow(ctx, run.GetID(), run.GetRunID(), "status")
			if e != nil {
				return false
			}
			var s model.RunState
			return value.Get(&s) == nil && s.Status == "paused" && s.PausedStep != nil && s.PausedStep.Budget != nil
		}, 15*time.Second, 100*time.Millisecond)
	}
	resume := func() {
		require.NoError(t, c.SignalWorkflow(ctx, run.GetID(), run.GetRunID(), "resume", model.WorkflowEvent{StepID: "first", Actor: "test-admin"}))
	}
	paused()
	require.Equal(t, int32(0), calls.Load(), "hard-zero prevents provider dispatch")
	p.HardUSD = "0.002"
	p, err = b.SaveBudgetPolicy(ctx, p, "test-admin")
	require.NoError(t, err)
	resume()
	require.Eventually(t, func() bool { return calls.Load() == 1 }, 10*time.Second, 100*time.Millisecond)
	paused()
	// Restart the worker while paused; the recorded successful response survives.
	w.Stop()
	w = newWorker()
	require.NoError(t, w.Start())
	paused()
	resume()
	time.Sleep(400 * time.Millisecond)
	paused()
	require.Equal(t, int32(1), calls.Load(), "recheck does not rebill the completed node")
	p.HardUSD = "1"
	p, err = b.SaveBudgetPolicy(ctx, p, "test-admin")
	require.NoError(t, err)
	resume()
	var result model.RunResult
	require.NoError(t, run.Get(ctx, &result))
	require.Equal(t, "retained response", result.Output)
	require.Equal(t, int32(2), calls.Load())

	// Soft-only policies log without stopping, including above-threshold calls.
	p.HardUSD = ""
	p, err = b.SaveBudgetPolicy(ctx, p, "test-admin")
	require.NoError(t, err)
	softRun, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{ID: team + "-soft-" + uuid.NewString(), TaskQueue: queue}, engine.RunloomWorkflow, model.RunInput{Definition: def, TeamID: team, Source: "schedule", Input: map[string]any{}})
	require.NoError(t, err)
	require.NoError(t, softRun.Get(ctx, &result))
	require.Equal(t, int32(4), calls.Load())
	// An audited workflow exclusion bypasses a zero hard limit, not accounting.
	_, err = b.SaveDraft(def)
	require.NoError(t, err)
	p.HardUSD = "0"
	p.ExcludedWorkflows = []string{def.ID}
	_, err = b.SaveBudgetPolicy(ctx, p, "test-admin")
	require.NoError(t, err)
	excludedRun, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{ID: team + "-excluded-" + uuid.NewString(), TaskQueue: queue}, engine.RunloomWorkflow, model.RunInput{Definition: def, TeamID: team, Source: "schedule", Input: map[string]any{}})
	require.NoError(t, err)
	require.NoError(t, excludedRun.Get(ctx, &result))
	require.Equal(t, int32(6), calls.Load())
	entries, err := b.QueryUsage(ctx, usage.Filter{All: true, Limit: 100})
	require.NoError(t, err)
	require.Len(t, entries, 6)
	for _, e := range entries {
		require.Equal(t, "default", e.ProjectID)
	}
	events, err := b.ListAudit(run.GetID(), "", 100)
	require.NoError(t, err)
	require.NotEmpty(t, events)
	history := &historypb.History{}
	it := c.GetWorkflowHistory(ctx, run.GetID(), run.GetRunID(), false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	for it.HasNext() {
		event, e := it.Next()
		require.NoError(t, e)
		history.Events = append(history.Events, event)
	}
	replay := worker.NewWorkflowReplayer()
	replay.RegisterWorkflow(engine.RunloomWorkflow)
	require.NoError(t, replay.ReplayWorkflowHistory(nil, history))
	require.Equal(t, int32(6), calls.Load())
}
