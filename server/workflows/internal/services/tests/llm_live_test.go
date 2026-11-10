package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/example/runloom/workflows/internal/api"
	"github.com/example/runloom/workflows/internal/services"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/runloom/workflows/internal/activities"
	"github.com/example/runloom/workflows/internal/budget"
	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/engine"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/protection"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/example/runloom/workflows/internal/usage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/activity"
	temporalclient "go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

// Opt-in local Temporal test: isolated team/queue and loopback fake LLM only.
// No paid API, provider account or production worker configuration is touched.
func TestServiceLLMUsageLive(t *testing.T) {
	address := os.Getenv("RUNLOOM_TEST_TEMPORAL_ADDRESS")
	if address == "" || os.Getenv("RUNLOOM_TEST_SERVICE_DATABASE") == "" {
		t.Skip("set local service database and Temporal test settings")
	}
	host, _, err := net.SplitHostPort(address)
	require.NoError(t, err)
	require.Contains(t, []string{"localhost", "127.0.0.1", "::1"}, host, "service verification is local-only")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	t.Setenv("RUNLOOM_SECRETS_KEY_FILE", filepath.Join(os.Getenv("RUNLOOM_TEST_SERVICE_CONFIG_DIR"), "workflow-key"))
	namespace := os.Getenv("RUNLOOM_TEST_NAMESPACE")
	require.NotEmpty(t, namespace)
	clientOptions, err := protection.TemporalOptions(address, namespace)
	require.NoError(t, err)
	c, err := temporalclient.Dial(clientOptions)
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
	team := os.Getenv("RUNLOOM_TEST_TEAM_ID")
	require.NotEmpty(t, team, "live verification requires an authorized synthetic team")
	queue := "runloom-usage-test-" + team
	data := t.TempDir()
	driver := os.Getenv("RUNLOOM_TEST_SERVICE_DATABASE")
	require.Contains(t, []string{"mongodb", "postgres"}, driver)
	directory := os.Getenv("RUNLOOM_TEST_SERVICE_CONFIG_DIR")
	connection := func(owner string) string {
		raw, err := os.ReadFile(filepath.Join(directory, owner+"-database-url"))
		require.NoError(t, err)
		u, err := url.Parse(strings.TrimSpace(string(raw)))
		require.NoError(t, err)
		if driver == "mongodb" {
			require.Equal(t, "mongodb:27017", u.Host)
			u.Host = "localhost:37017"
			q := u.Query()
			q.Set("directConnection", "true")
			u.RawQuery = q.Encode()
		} else {
			require.Equal(t, "postgres:5432", u.Host)
			u.Host = "localhost:36432"
		}
		return u.String()
	}
	workflowURL := connection("workflows")
	rawOpen := func(ctx context.Context, id string) (store.Backend, error) {
		return store.OpenTeam(ctx, driver, workflowURL, "runloom_workflows", data, id)
	}
	managementURL := os.Getenv("RUNLOOM_TEST_MANAGEMENT_URL")
	managementAddress, err := url.Parse(managementURL)
	require.NoError(t, err)
	require.Contains(t, []string{"localhost", "127.0.0.1"}, managementAddress.Hostname())
	remote, err := services.NewClient(managementURL, "worker", filepath.Join(directory, "worker-token"))
	require.NoError(t, err)
	openTeam := func(ctx context.Context, id string) (store.Backend, error) {
		local, err := rawOpen(ctx, id)
		if err != nil {
			return nil, err
		}
		return &services.Backend{Backend: local, Role: "worker", Team: id, Management: remote}, nil
	}
	b, err := openTeam(ctx, team)
	require.NoError(t, err)
	defer b.Close(context.Background())
	prices, err := usage.Load("../../../testdata/deploy/llm-pricing.example.json")
	require.NoError(t, err)
	meter := activities.MeteredLLM{OpenTeam: func(ctx context.Context, id string) (activities.MeteringStore, error) { return openTeam(ctx, id) }, Prices: prices, Provider: "local-test", Ancestors: activities.LLMAncestors(c)}
	recorder := activities.RunRecorder{OpenTeam: func(ctx context.Context, id string) (activities.RecordingStore, error) { return openTeam(ctx, id) }}
	w := worker.New(c, queue, worker.Options{})
	w.RegisterWorkflow(engine.RunloomWorkflow)
	w.RegisterActivityWithOptions(meter.Call, activity.RegisterOptions{Name: "runloom.llm"})
	w.RegisterActivityWithOptions(meter.Check, activity.RegisterOptions{Name: "runloom.internal.check_llm_budget"})
	w.RegisterActivityWithOptions(recorder.Record, activity.RegisterOptions{Name: "runloom.internal.record_run"})
	require.NoError(t, w.Start())
	defer w.Stop()
	s := api.New(b, c, queue)
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
	// Usage must be stored only in Management, not copied into the workflow database.
	local, err := rawOpen(ctx, team)
	require.NoError(t, err)
	empty, err := local.QueryUsage(ctx, usage.Filter{RunKey: usage.RunKey(run.ID, run.RunID)})
	require.NoError(t, err)
	require.Empty(t, empty)
	require.NoError(t, local.Close(ctx))
	// Replay consumes recorded commands/results without another provider request.
	history := &historypb.History{}
	iterator := c.GetWorkflowHistory(ctx, run.ID, run.RunID, false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	for iterator.HasNext() {
		event, err := iterator.Next()
		require.NoError(t, err)
		history.Events = append(history.Events, event)
	}
	replayer, err := worker.NewWorkflowReplayerWithOptions(worker.WorkflowReplayerOptions{DataConverter: clientOptions.DataConverter, FailureConverter: clientOptions.FailureConverter})
	require.NoError(t, err)
	replayer.RegisterWorkflow(engine.RunloomWorkflow)
	require.NoError(t, replayer.ReplayWorkflowHistory(nil, history))
	require.Equal(t, int32(3), requests.Load())

	// A policy owned by Management pauses execution rather than cancelling it.
	savePolicy := func(p budget.Policy) (budget.Policy, error) {
		raw, err := json.Marshal(p)
		if err != nil {
			return p, err
		}
		endpoint := os.Getenv("RUNLOOM_UI_URL") + "/api/v1/studio/orgs/" + os.Getenv("RUNLOOM_TEST_ORG_ID") + "/teams/" + team + "/llm-budget-policies/" + p.ID
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, strings.NewReader(string(raw)))
		if err != nil {
			return p, err
		}
		req.Header.Set("Authorization", os.Getenv("RUNLOOM_TEST_AUTHORIZATION"))
		req.Header.Set("Content-Type", "application/json")
		response, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
		if err != nil {
			return p, fmt.Errorf("policy API unavailable")
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return p, fmt.Errorf("policy API status %d", response.StatusCode)
		}
		err = json.NewDecoder(response.Body).Decode(&p)
		return p, err
	}
	policy, err := savePolicy(budget.Policy{ID: uuid.NewString(), Name: "Live service budget", Enabled: true, SoftUSD: "0", HardUSD: "0", Reason: "Synthetic service verification"})
	require.NoError(t, err)
	d, err := definition.Normalize(model.Definition{SchemaVersion: "1.0", ID: "budget-live", Name: "Service budget", Steps: []model.Step{llm}, Output: "${steps.ask.output.content}"})
	require.NoError(t, err)
	budgetRun, err := c.ExecuteWorkflow(ctx, temporalclient.StartWorkflowOptions{ID: team + "-budget-" + uuid.NewString(), TaskQueue: queue}, engine.RunloomWorkflow, model.RunInput{Definition: d, TeamID: team, Source: "schedule", Input: map[string]any{}})
	require.NoError(t, err)
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = c.CancelWorkflow(cleanup, budgetRun.GetID(), budgetRun.GetRunID())
	}()
	paused := func() {
		require.Eventually(t, func() bool {
			v, e := c.QueryWorkflow(ctx, budgetRun.GetID(), budgetRun.GetRunID(), "status")
			if e != nil {
				return false
			}
			var state model.RunState
			return v.Get(&state) == nil && state.Status == "paused" && state.PausedStep != nil && state.PausedStep.Budget != nil
		}, 10*time.Second, 100*time.Millisecond)
	}
	resume := func() {
		require.NoError(t, c.SignalWorkflow(ctx, budgetRun.GetID(), budgetRun.GetRunID(), "resume", model.WorkflowEvent{StepID: "ask", Actor: "test-admin"}))
	}
	paused()
	require.Equal(t, int32(3), requests.Load(), "zero hard limit must prevent dispatch")
	policy.HardUSD = "0.007" // Previous parent/child requests spent 0.006.
	policy, err = savePolicy(policy)
	require.NoError(t, err)
	resume()
	require.Eventually(t, func() bool { return requests.Load() == 4 }, 10*time.Second, 100*time.Millisecond)
	paused()
	resume()
	time.Sleep(300 * time.Millisecond)
	paused()
	require.Equal(t, int32(4), requests.Load(), "blocked resume must not rebill the retained response")
	policy.HardUSD = "1"
	_, err = savePolicy(policy)
	require.NoError(t, err)
	resume()
	require.NoError(t, budgetRun.Get(ctx, &result))
	require.Equal(t, int32(4), requests.Load(), "resuming after policy change reuses the completed LLM result")
	events, err := b.ListAudit(budgetRun.GetID(), "", 100)
	require.NoError(t, err)
	require.NotEmpty(t, events)
}
