package engine

import (
	"context"
	"github.com/example/runloom/workflows/internal/budget"
	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
	"testing"
	"time"
)

func TestBudgetPauseAndResume(t *testing.T) {
	for _, post := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-provider", true: "after-provider"}[post], func(t *testing.T) {
			suite := testsuite.WorkflowTestSuite{}
			env := suite.NewTestWorkflowEnvironment()
			calls, checks := 0, 0
			blocked := true
			d := budget.Decision{Reason: "hard_limit_reached", PolicyID: "policy", Message: "Project monthly budget exhausted"}
			env.RegisterActivityWithOptions(func(_ context.Context, p map[string]any) (map[string]any, error) {
				calls++
				require.Equal(t, true, p["_runloom_budget_v1"])
				if post {
					return map[string]any{"content": "preserved", "_runloom_budget_pause": d}, nil
				}
				if blocked {
					return nil, temporal.NewNonRetryableApplicationError(d.Message, budget.PauseError, nil, d)
				}
				return map[string]any{"content": "preserved"}, nil
			}, activity.RegisterOptions{Name: "runloom.llm"})
			env.RegisterActivityWithOptions(func(context.Context) (*budget.Decision, error) {
				checks++
				if blocked {
					return &d, nil
				}
				return nil, nil
			}, activity.RegisterOptions{Name: "runloom.internal.check_llm_budget"})
			assertPaused := func() {
				raw, err := env.QueryWorkflow("status")
				require.NoError(t, err)
				var state model.RunState
				require.NoError(t, raw.Get(&state))
				require.Equal(t, "paused", state.Status)
				require.NotNil(t, state.PausedStep.Budget)
				require.Equal(t, "hard_limit_reached", state.PausedStep.Budget.Reason)
				require.Empty(t, state.CompletedAt)
				require.Empty(t, state.CompletedSteps)
			}
			env.RegisterDelayedCallback(func() { assertPaused(); env.SignalWorkflow("resume", model.WorkflowEvent{StepID: "ask"}) }, time.Second)
			env.RegisterDelayedCallback(func() {
				assertPaused()
				blocked = false
				env.SignalWorkflow("resume", model.WorkflowEvent{StepID: "ask"})
			}, 2*time.Second)
			def, err := definition.Normalize(model.Definition{SchemaVersion: "1.0", ID: "budget", Name: "Budget", Steps: []model.Step{{ID: "ask", Type: "llm", Prompt: "test", Model: "test"}}, Output: "${steps.ask.output.content}"})
			require.NoError(t, err)
			env.ExecuteWorkflow(RunloomWorkflow, model.RunInput{Definition: def})
			require.NoError(t, env.GetWorkflowError())
			var result model.RunResult
			require.NoError(t, env.GetWorkflowResult(&result))
			require.Equal(t, "preserved", result.Output)
			if post {
				require.Equal(t, 1, calls, "never repeat a paid call after resume")
				require.Equal(t, 2, checks)
			} else {
				require.Equal(t, 3, calls)
			}
		})
	}
}

func TestBudgetPauseCancellationAndOldVersion(t *testing.T) {
	for _, old := range []bool{false, true} {
		suite := testsuite.WorkflowTestSuite{}
		env := suite.NewTestWorkflowEnvironment()
		if old {
			env.OnGetVersion("llm-budget-pause-v1", workflow.DefaultVersion, workflow.Version(1)).Return(workflow.DefaultVersion)
		}
		env.RegisterActivityWithOptions(func(_ context.Context, p map[string]any) (map[string]any, error) {
			if old {
				require.NotContains(t, p, "_runloom_budget_v1")
				return map[string]any{"content": "legacy"}, nil
			}
			return nil, temporal.NewNonRetryableApplicationError("blocked", budget.PauseError, nil, budget.Decision{Reason: "hard_limit_reached", Message: "blocked"})
		}, activity.RegisterOptions{Name: "runloom.llm"})
		if !old {
			env.RegisterDelayedCallback(func() { env.CancelWorkflow() }, time.Second)
		}
		def, err := definition.Normalize(model.Definition{SchemaVersion: "1.0", ID: "budget", Name: "Budget", Steps: []model.Step{{ID: "ask", Type: "activity", Activity: "runloom.llm", With: map[string]any{}}}})
		require.NoError(t, err)
		env.ExecuteWorkflow(RunloomWorkflow, model.RunInput{Definition: def})
		if old {
			require.NoError(t, env.GetWorkflowError())
		} else {
			require.True(t, temporal.IsCanceledError(env.GetWorkflowError()))
		}
	}
}

func TestBudgetPauseBlocksNewParallelSteps(t *testing.T) {
	suite := testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	blocked := true
	otherCalls := 0
	env.RegisterActivityWithOptions(func(context.Context, map[string]any) (map[string]any, error) {
		if blocked {
			return nil, temporal.NewNonRetryableApplicationError("blocked", budget.PauseError, nil, budget.Decision{Reason: "hard_limit_reached", Message: "blocked"})
		}
		return map[string]any{}, nil
	}, activity.RegisterOptions{Name: "runloom.llm"})
	env.RegisterActivityWithOptions(func(context.Context, map[string]any) (map[string]any, error) {
		otherCalls++
		return map[string]any{}, nil
	}, activity.RegisterOptions{Name: "runloom.echo"})
	def, err := definition.Normalize(model.Definition{SchemaVersion: "1.0", ID: "parallel-budget", Name: "Parallel budget", Steps: []model.Step{{ID: "parallel", Type: "parallel", Branches: []model.Branch{{ID: "ai", Steps: []model.Step{{ID: "ask", Type: "llm", Model: "test", Prompt: "test"}}}, {ID: "other", Steps: []model.Step{{ID: "wait", Type: "wait", Seconds: 2}, {ID: "echo", Type: "activity", Activity: "runloom.echo"}}}}}}})
	require.NoError(t, err)
	env.RegisterDelayedCallback(func() {
		require.Equal(t, 0, otherCalls)
		value, err := env.QueryWorkflow("status")
		require.NoError(t, err)
		var state model.RunState
		require.NoError(t, value.Get(&state))
		require.Equal(t, "paused", state.Status)
		blocked = false
		env.SignalWorkflow("resume", model.WorkflowEvent{StepID: "ask"})
	}, 4*time.Second)
	env.ExecuteWorkflow(RunloomWorkflow, model.RunInput{Definition: def})
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 1, otherCalls)
}
