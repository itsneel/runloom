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
	"github.com/example/runloom/workflows/internal/prompts"
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

// Opt-in, disposable Temporal and filesystem storage. Only a loopback fake
// provider is called. Exercises real API publish/start/status, accounting and replay.
func TestDecisionLive(t *testing.T) {
	address := os.Getenv("RUNLOOM_TEST_TEMPORAL_ADDRESS")
	if address == "" {
		t.Skip("set RUNLOOM_TEST_TEMPORAL_ADDRESS to a disposable local server")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c, err := client.Dial(client.Options{HostPort: address})
	require.NoError(t, err)
	defer c.Close()
	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "json_schema", body["response_format"].(map[string]any)["type"])
		if body["response_format"].(map[string]any)["json_schema"].(map[string]any)["name"] == "runloom_llm_v1" {
			require.Equal(t, "Draft a response for synthetic", body["messages"].([]any)[1].(map[string]any)["content"])
			_ = json.NewEncoder(w).Encode(map[string]any{"model": "test-model", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": `{"content":"Draft for review","hitl_required":true,"hitl_reason":"Confirm the destination"}`}}}, "usage": map[string]any{"prompt_tokens": 1000, "completion_tokens": 500}})
			return
		}
		text := body["messages"].([]any)[1].(map[string]any)["content"].(string)
		var cfg struct {
			Mode      string `json:"mode"`
			Remaining int    `json:"remaining"`
			Actions   []struct {
				ID string `json:"id"`
			} `json:"actions"`
			History []any `json:"history"`
		}
		parts := strings.SplitN(text, "Available actions, limits, and previous results (JSON data):\n", 2)
		require.Len(t, parts, 2)
		require.NoError(t, json.Unmarshal([]byte(parts[1]), &cfg))
		outcome, selected := "actions", []string{cfg.Actions[0].ID}
		if cfg.Mode == "multiple" {
			selected = append(selected, cfg.Actions[1].ID)
		}
		if cfg.Mode == "agent" && len(cfg.History) >= 2 {
			outcome, selected = "finish", []string{}
		}
		content, _ := json.Marshal(map[string]any{"outcome": outcome, "actions": selected, "reason": "Synthetic verified choice", "hitl_required": false, "hitl_reason": ""})
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "test-model", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": string(content)}}}, "usage": map[string]any{"prompt_tokens": 1000, "completion_tokens": 500}})
	}))
	defer provider.Close()
	t.Setenv("RUNLOOM_LLM_API_KEY_FILE", "")
	t.Setenv("RUNLOOM_LLM_API_KEY", "synthetic-local-only")
	t.Setenv("RUNLOOM_LLM_DEFAULT_MODEL", "test-model")
	t.Setenv("RUNLOOM_LLM_BASE_URL", provider.URL)
	team := uuid.NewString()
	queue := "decision-test-" + team
	data := t.TempDir()
	open := func(ctx context.Context, id string) (store.Backend, error) {
		return store.OpenTeam(ctx, "file", "", "", data, id)
	}
	b, err := open(ctx, team)
	require.NoError(t, err)
	defer b.Close(context.Background())
	prices, err := usage.Load("../../testdata/deploy/llm-pricing.example.json")
	require.NoError(t, err)
	meter := activities.MeteredLLM{OpenTeam: func(ctx context.Context, id string) (activities.MeteringStore, error) { return open(ctx, id) }, Prices: prices, Provider: "local-test", Ancestors: activities.LLMAncestors(c)}
	recorder := activities.RunRecorder{OpenTeam: func(ctx context.Context, id string) (activities.RecordingStore, error) { return open(ctx, id) }}
	w := worker.New(c, queue, worker.Options{})
	w.RegisterWorkflow(engine.RunloomWorkflow)
	w.RegisterActivityWithOptions(meter.Call, activity.RegisterOptions{Name: "runloom.llm"})
	w.RegisterActivityWithOptions(recorder.Record, activity.RegisterOptions{Name: "runloom.internal.record_run"})
	w.RegisterActivityWithOptions(activities.Echo, activity.RegisterOptions{Name: "runloom.echo"})
	w.RegisterActivityWithOptions(activities.ValidateOutput, activity.RegisterOptions{Name: "runloom.internal.validate_output"})
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
		r.Header.Set("X-Runloom-Actor", "decision-test")
		response := httptest.NewRecorder()
		s.Handler().ServeHTTP(response, r)
		require.Equal(t, status, response.Code, response.Body.String())
		return response
	}
	raw, err := os.ReadFile("../../testdata/examples/ai-decision-modes.json")
	require.NoError(t, err)
	var d model.Definition
	require.NoError(t, json.Unmarshal(raw, &d))
	child := model.Definition{SchemaVersion: "1.0", ID: "decision-child", Kind: "child", Name: "Decision child", Steps: []model.Step{{ID: "echo", Type: "activity", Activity: "runloom.echo", With: map[string]any{"value": "${input.value}"}}}, Output: "${steps.echo.output}"}
	call("POST", "/api/v1/studio/definitions", child, 200)
	call("POST", "/api/v1/studio/definitions/decision-child/publish", map[string]any{}, 200)
	d.Steps[2].Branches[0].Steps = []model.Step{{ID: "call-child", Type: "subworkflow", DefinitionID: child.ID, Version: 1, With: map[string]any{"value": "${decision.round}"}}}
	prompt := prompts.Entry{ID: "draft-response", Name: "Draft response", Template: prompts.Template{Text: "Draft a response for ${vars.text}", Variables: []prompts.Variable{{Name: "text", Type: "string", Required: true}}}}
	raw = call("POST", "/api/v1/studio/prompts/draft-response/publish", prompt, 200).Body.Bytes()
	require.NoError(t, json.Unmarshal(raw, &prompt))
	d.Steps = append(d.Steps, model.Step{ID: "review-answer", Type: "llm", Model: "test-model", PromptRef: &prompts.Reference{ID: prompt.ID, Version: 1}, PromptInputs: map[string]any{"text": "${input.text}"}, OutputSchema: map[string]any{"type": "object", "required": []any{"content", "hitl_required", "prompt_hash"}}})
	call("POST", "/api/v1/studio/definitions", d, 200)
	call("POST", "/api/v1/studio/definitions/"+d.ID+"/publish", map[string]any{}, 200)
	prompt.Template.Text = "New library version must not change the published workflow: ${vars.text}"
	call("POST", "/api/v1/studio/prompts/draft-response/publish", prompt, 200)
	started := call("POST", "/api/v1/studio/runs", map[string]any{"definition_id": d.ID, "version": 1, "input": map[string]any{"text": "synthetic"}}, 201)
	var run struct {
		ID    string `json:"workflow_id"`
		RunID string `json:"run_id"`
	}
	require.NoError(t, json.Unmarshal(started.Body.Bytes(), &run))
	var result model.RunResult
	require.NoError(t, c.GetWorkflow(ctx, run.ID, run.RunID).Get(ctx, &result))
	require.Equal(t, int32(6), requests.Load())
	status := call("GET", "/api/v1/studio/runs/"+run.ID, nil, 200)
	var snapshot struct {
		State model.RunState `json:"state"`
	}
	require.NoError(t, json.Unmarshal(status.Body.Bytes(), &snapshot))
	answer := snapshot.State.Steps["review-answer"].Output.(map[string]any)
	require.Equal(t, true, answer["hitl_required"])
	require.Equal(t, "Confirm the destination", answer["hitl_reason"])
	require.Equal(t, "Draft for review", answer["content"])
	require.Equal(t, float64(1), answer["prompt_ref"].(map[string]any)["version"])
	for _, step := range d.Steps[:3] {
		out := snapshot.State.Steps[step.ID].Output.(map[string]any)
		require.Equal(t, false, out["hitl_required"])
		for _, round := range out["rounds"].([]any) {
			require.Equal(t, false, round.(map[string]any)["hitl_required"])
		}
	}
	require.Len(t, snapshot.State.ChildWorkflows, 2)
	ids := map[string]bool{}
	for _, id := range snapshot.State.ChildWorkflows {
		require.False(t, ids[id])
		ids[id] = true
	}
	page := call("GET", "/api/v1/studio/runs/"+run.ID+"/llm-usage", nil, 200)
	var cost struct {
		Entries []usage.Entry `json:"entries"`
	}
	require.NoError(t, json.Unmarshal(page.Body.Bytes(), &cost))
	require.Len(t, cost.Entries, 6)
	events, err := b.ListAudit(run.ID, "", 200)
	require.NoError(t, err)
	decisions := 0
	for _, e := range events {
		if e.EventType == "llm.decision_selected" {
			decisions++
		}
	}
	require.Equal(t, 5, decisions)
	history := &historypb.History{}
	iterator := c.GetWorkflowHistory(ctx, run.ID, run.RunID, false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	for iterator.HasNext() {
		e, err := iterator.Next()
		require.NoError(t, err)
		history.Events = append(history.Events, e)
	}
	replayer := worker.NewWorkflowReplayer()
	replayer.RegisterWorkflow(engine.RunloomWorkflow)
	require.NoError(t, replayer.ReplayWorkflowHistory(nil, history))
	require.Equal(t, int32(6), requests.Load(), "replay must not make a new model request")
}
