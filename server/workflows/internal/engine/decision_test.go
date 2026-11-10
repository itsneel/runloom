package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/example/runloom/workflows/internal/budget"
	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func TestDecisionLegacyHITLVersion(t *testing.T) {
	suite := testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	env.OnGetVersion("decision-hitl-contract-v1", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	env.RegisterActivityWithOptions(func(context.Context, map[string]any) (map[string]any, error) {
		return decisionOutput(`{"outcome":"finish","actions":[],"reason":"Previously recorded answer"}`), nil
	}, activity.RegisterOptions{Name: "runloom.llm"})
	env.ExecuteWorkflow(RunloomWorkflow, model.RunInput{Definition: decisionFixture(t, "single", "sequential"), Input: map[string]any{"text": "test"}})
	require.NoError(t, env.GetWorkflowError())
	// Historical results must not acquire a fabricated assessment.
	require.NotContains(t, decisionState(t, env).Steps["route"].Output.(map[string]any), "hitl_required")
}

func decisionOutput(content string) map[string]any {
	var data any
	_ = json.Unmarshal([]byte(content), &data)
	return map[string]any{"content": content, "data": data}
}

func decisionFixture(t *testing.T, mode, execution string) model.Definition {
	t.Helper()
	s := model.Step{ID: "route", Type: "decision", Model: "test", Prompt: "Request: ${input.text}", Decision: &model.DecisionConfig{Mode: mode, Execution: execution}, Branches: []model.Branch{
		{ID: "a", Description: "Action A", Steps: []model.Step{{ID: "do-a", Type: "activity", Activity: "runloom.echo", With: map[string]any{"action": "a", "round": "${decision.round}"}}}},
		{ID: "b", Description: "Action B", Steps: []model.Step{{ID: "do-b", Type: "activity", Activity: "runloom.echo", With: map[string]any{"action": "b", "round": "${decision.round}"}}}},
	}}
	d, err := definition.Normalize(model.Definition{SchemaVersion: "1.0", ID: "decision-test", Name: "Decision", Steps: []model.Step{s}, Output: "${steps.route.output}"})
	require.NoError(t, err)
	return d
}
func decisionState(t *testing.T, env *testsuite.TestWorkflowEnvironment) model.RunState {
	t.Helper()
	raw, err := env.QueryWorkflow("status")
	require.NoError(t, err)
	var s model.RunState
	require.NoError(t, raw.Get(&s))
	return s
}
func TestDecisionModes(t *testing.T) {
	for _, tc := range []struct {
		mode, execution string
		choices         []string
		want            []string
	}{
		{"single", "sequential", []string{`{"hitl_required":false,"hitl_reason":"","outcome":"actions","actions":["b"],"reason":"B fits"}`}, []string{"b"}},
		{"multiple", "sequential", []string{`{"hitl_required":false,"hitl_reason":"","outcome":"actions","actions":["b","a"],"reason":"Both"}`}, []string{"b", "a"}},
		{"multiple", "parallel", []string{`{"hitl_required":false,"hitl_reason":"","outcome":"actions","actions":["b","a"],"reason":"Both"}`}, []string{"b", "a"}},
		{"agent", "sequential", []string{`{"hitl_required":false,"hitl_reason":"","outcome":"actions","actions":["a"],"reason":"Start"}`, `{"hitl_required":false,"hitl_reason":"","outcome":"actions","actions":["a"],"reason":"Again"}`, `{"hitl_required":false,"hitl_reason":"","outcome":"finish","actions":[],"reason":"Done"}`}, []string{"a", "a"}},
	} {
		t.Run(tc.mode+tc.execution, func(t *testing.T) {
			suite := testsuite.WorkflowTestSuite{}
			env := suite.NewTestWorkflowEnvironment()
			calls := 0
			env.RegisterActivityWithOptions(func(_ context.Context, p map[string]any) (map[string]any, error) {
				cfg := p["_runloom_decision"].(map[string]any)
				history := cfg["history"].([]any)
				require.Len(t, history, calls)
				if calls > 0 {
					require.NotEmpty(t, history[calls-1].(map[string]any)["results"])
				}
				require.Equal(t, "route", p["_runloom_step_id"])
				require.Equal(t, true, p["_runloom_budget_v1"])
				content := tc.choices[calls]
				calls++
				return decisionOutput(content), nil
			}, activity.RegisterOptions{Name: "runloom.llm"})
			var mu sync.Mutex
			executed := []string{}
			env.RegisterActivityWithOptions(func(_ context.Context, p map[string]any) (map[string]any, error) {
				mu.Lock()
				defer mu.Unlock()
				executed = append(executed, p["action"].(string))
				return p, nil
			}, activity.RegisterOptions{Name: "runloom.echo"})
			d := decisionFixture(t, tc.mode, tc.execution)
			if tc.mode == "agent" {
				d.Steps[0].Decision.MaxActions = 2
			}
			env.ExecuteWorkflow(RunloomWorkflow, model.RunInput{Definition: d, Input: map[string]any{"text": "test"}})
			require.NoError(t, env.GetWorkflowError())
			require.Equal(t, len(tc.choices), calls)
			if tc.execution == "parallel" {
				require.ElementsMatch(t, tc.want, executed)
			} else {
				require.Equal(t, tc.want, executed)
			}
			var result model.RunResult
			require.NoError(t, env.GetWorkflowResult(&result))
			out := result.Output.(map[string]any)
			require.Equal(t, float64(len(tc.want)), out["actions_executed"])
			state := decisionState(t, env)
			require.Len(t, state.Steps, len(tc.want)+1, "invocations must have distinct state IDs")
		})
	}
}

func TestDecisionFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name, mode, body string
		rounds           int
		wantCalls        int
	}{
		{"unknown", "single", `{"hitl_required":false,"hitl_reason":"","outcome":"actions","actions":["other"],"reason":"Try"}`, 1, 0},
		{"duplicate", "multiple", `{"hitl_required":false,"hitl_reason":"","outcome":"actions","actions":["a","a"],"reason":"Try"}`, 1, 0},
		{"too-many", "single", `{"hitl_required":false,"hitl_reason":"","outcome":"actions","actions":["a","b"],"reason":"Try"}`, 1, 0},
		{"malformed", "single", "not json", 1, 0},
		{"round-limit", "agent", `{"hitl_required":false,"hitl_reason":"","outcome":"actions","actions":["a"],"reason":"Again"}`, 2, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			suite := testsuite.WorkflowTestSuite{}
			env := suite.NewTestWorkflowEnvironment()
			executed := 0
			env.RegisterActivityWithOptions(func(context.Context, map[string]any) (map[string]any, error) {
				return decisionOutput(tc.body), nil
			}, activity.RegisterOptions{Name: "runloom.llm"})
			env.RegisterActivityWithOptions(func(_ context.Context, p map[string]any) (map[string]any, error) { executed++; return p, nil }, activity.RegisterOptions{Name: "runloom.echo"})
			d := decisionFixture(t, tc.mode, "sequential")
			d.Steps[0].Decision.MaxRounds = tc.rounds
			env.ExecuteWorkflow(RunloomWorkflow, model.RunInput{Definition: d, Input: map[string]any{"text": "test"}})
			require.Error(t, env.GetWorkflowError())
			require.Equal(t, tc.wantCalls, executed)
			require.Equal(t, "failed", decisionState(t, env).Steps["route"].Status)
		})
	}
}

func TestDecisionHandoffAndCancel(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprint(cancel), func(t *testing.T) {
			suite := testsuite.WorkflowTestSuite{}
			env := suite.NewTestWorkflowEnvironment()
			calls := 0
			env.RegisterActivityWithOptions(func(context.Context, map[string]any) (map[string]any, error) {
				calls++
				return decisionOutput(`{"hitl_required":true,"hitl_reason":"Human review requested","outcome":"handoff","actions":[],"reason":"Human review needed"}`), nil
			}, activity.RegisterOptions{Name: "runloom.llm"})
			env.RegisterDelayedCallback(func() {
				s := decisionState(t, env)
				require.Equal(t, "paused", s.Status)
				require.Contains(t, s.PausedStep.Message, "Human review requested")
				require.Equal(t, true, s.Steps["route"].Output.(map[string]any)["hitl_required"])
				if cancel {
					env.CancelWorkflow()
				} else {
					env.SignalWorkflow("resume", model.WorkflowEvent{StepID: "route"})
				}
			}, time.Second)
			env.ExecuteWorkflow(RunloomWorkflow, model.RunInput{Definition: decisionFixture(t, "single", "sequential"), Input: map[string]any{"text": "test"}})
			if cancel {
				require.True(t, temporal.IsCanceledError(env.GetWorkflowError()))
			} else {
				require.NoError(t, env.GetWorkflowError())
				require.Equal(t, true, decisionState(t, env).Steps["route"].Output.(map[string]any)["hitl_required"])
			}
			require.Equal(t, 1, calls)
		})
	}
}

func TestDecisionBudgetKeepsPaidChoice(t *testing.T) {
	suite := testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	calls, actions := 0, 0
	env.RegisterActivityWithOptions(func(context.Context, map[string]any) (map[string]any, error) {
		calls++
		out := decisionOutput(`{"hitl_required":false,"hitl_reason":"","outcome":"actions","actions":["a"],"reason":"Selected"}`)
		out["_runloom_budget_pause"] = budget.Decision{Reason: "hard_limit_reached", Message: "Budget exhausted"}
		return out, nil
	}, activity.RegisterOptions{Name: "runloom.llm"})
	env.RegisterActivityWithOptions(func(context.Context) (*budget.Decision, error) { return nil, nil }, activity.RegisterOptions{Name: "runloom.internal.check_llm_budget"})
	env.RegisterActivityWithOptions(func(_ context.Context, p map[string]any) (map[string]any, error) { actions++; return p, nil }, activity.RegisterOptions{Name: "runloom.echo"})
	env.RegisterDelayedCallback(func() {
		require.Equal(t, 0, actions)
		require.Equal(t, "paused", decisionState(t, env).Status)
		env.SignalWorkflow("resume", model.WorkflowEvent{StepID: "route"})
	}, time.Second)
	env.ExecuteWorkflow(RunloomWorkflow, model.RunInput{Definition: decisionFixture(t, "single", "sequential"), Input: map[string]any{"text": "test"}})
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 1, calls)
	require.Equal(t, 1, actions)
}

func TestDecisionRepeatedCallbacksHaveFreshIDs(t *testing.T) {
	suite := testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	calls := 0
	env.RegisterActivityWithOptions(func(context.Context, map[string]any) (map[string]any, error) {
		calls++
		body := `{"hitl_required":false,"hitl_reason":"","outcome":"actions","actions":["a"],"reason":"Wait"}`
		if calls == 3 {
			body = `{"hitl_required":false,"hitl_reason":"","outcome":"finish","actions":[],"reason":"Done"}`
		}
		return decisionOutput(body), nil
	}, activity.RegisterOptions{Name: "runloom.llm"})
	d := decisionFixture(t, "agent", "sequential")
	d.Steps[0].Branches[0].Steps = []model.Step{{ID: "reply", Type: "callback", CallbackKey: "ready", Prompt: "Wait", OnTimeout: "fail"}}
	var first string
	env.RegisterDelayedCallback(func() {
		s := decisionState(t, env)
		require.Len(t, s.WaitingCallbacks, 1)
		for id := range s.WaitingCallbacks {
			first = id
			env.SignalWorkflow("callback", model.WorkflowEvent{StepID: id, Payload: "first"})
		}
	}, time.Second)
	env.RegisterDelayedCallback(func() {
		s := decisionState(t, env)
		require.Len(t, s.WaitingCallbacks, 1)
		for id := range s.WaitingCallbacks {
			require.NotEqual(t, first, id)
			env.SignalWorkflow("callback", model.WorkflowEvent{StepID: first, Payload: "stale"})
		}
	}, 2*time.Second)
	env.RegisterDelayedCallback(func() {
		s := decisionState(t, env)
		require.Len(t, s.WaitingCallbacks, 1)
		for id := range s.WaitingCallbacks {
			env.SignalWorkflow("callback", model.WorkflowEvent{StepID: id, Payload: "second"})
		}
	}, 3*time.Second)
	env.ExecuteWorkflow(RunloomWorkflow, model.RunInput{Definition: d, Input: map[string]any{"text": "test"}})
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 3, calls)
}

func TestDecisionParallelFailureAndCancellation(t *testing.T) {
	for _, cancelRun := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelRun), func(t *testing.T) {
			suite := testsuite.WorkflowTestSuite{}
			env := suite.NewTestWorkflowEnvironment()
			env.RegisterActivityWithOptions(func(context.Context, map[string]any) (map[string]any, error) {
				return decisionOutput(`{"hitl_required":false,"hitl_reason":"","outcome":"actions","actions":["a","b"],"reason":"Both"}`), nil
			}, activity.RegisterOptions{Name: "runloom.llm"})
			d := decisionFixture(t, "multiple", "parallel")
			d.Steps[0].Branches[0].Steps = []model.Step{{ID: "wait-a", Type: "wait", Seconds: 3600}}
			if cancelRun {
				d.Steps[0].Branches[1].Steps = []model.Step{{ID: "wait-b", Type: "wait", Seconds: 3600}}
				env.RegisterDelayedCallback(env.CancelWorkflow, time.Second)
			} else {
				env.RegisterActivityWithOptions(func(context.Context, map[string]any) (map[string]any, error) {
					return nil, temporal.NewNonRetryableApplicationError("action failed", "ActionFailed", nil)
				}, activity.RegisterOptions{Name: "runloom.echo"})
			}
			env.ExecuteWorkflow(RunloomWorkflow, model.RunInput{Definition: d, Input: map[string]any{"text": "test"}})
			require.Error(t, env.GetWorkflowError())
			if cancelRun {
				require.True(t, temporal.IsCanceledError(env.GetWorkflowError()))
			} else {
				require.Contains(t, env.GetWorkflowError().Error(), "action failed")
			}
		})
	}
}
