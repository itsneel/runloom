package workflows_test

import (
	"context"
	"testing"
	"time"

	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/engine"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func loopDefinition() model.Definition {
	return model.Definition{ID: "loop-test", Name: "Loop", SchemaVersion: "1.0", Steps: []model.Step{{
		ID: "each", Type: "foreach", ForEach: &model.ForEach{Items: "${input.items}", Output: "${steps.map.output}", Concurrency: 2},
		Branches: []model.Branch{{ID: "body", Steps: []model.Step{{ID: "map", Type: "transform", Data: &model.DataOperation{Operation: "map", Fields: []model.FieldMapping{{Name: "value", Value: "${loop.item}"}, {Name: "position", Value: "${loop.index}"}}}}}}},
	}}, Output: "${steps.each.output}"}
}
func TestForEachCollectsInOrderAndDoesNotLeakBodySteps(t *testing.T) {
	require.NotContains(t, (definition.Context{}).Value(), "loop", "existing non-loop expression behavior must remain unchanged")
	d, err := definition.Normalize(loopDefinition())
	require.NoError(t, err)
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.ExecuteWorkflow(engine.RunloomWorkflow, model.RunInput{Definition: d, Input: map[string]any{"items": []any{"first", "second", "third"}}})
	require.NoError(t, env.GetWorkflowError())
	var result model.RunResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, []any{map[string]any{"value": "first", "position": float64(0)}, map[string]any{"value": "second", "position": float64(1)}, map[string]any{"value": "third", "position": float64(2)}}, result.Output)
	require.NotContains(t, result.Steps, "map")
	hints, err := definition.VariableHints(d, "map")
	require.NoError(t, err)
	found := false
	for _, h := range hints {
		if h.Path == "loop.item" {
			found = true
		}
	}
	require.True(t, found)
}
func TestForEachEmptyAndLimit(t *testing.T) {
	for _, count := range []int{0, 101} {
		d, err := definition.Normalize(loopDefinition())
		require.NoError(t, err)
		var suite testsuite.WorkflowTestSuite
		env := suite.NewTestWorkflowEnvironment()
		env.ExecuteWorkflow(engine.RunloomWorkflow, model.RunInput{Definition: d, Input: map[string]any{"items": make([]any, count)}})
		if count > 100 {
			require.Error(t, env.GetWorkflowError())
		} else {
			require.NoError(t, env.GetWorkflowError())
			var result model.RunResult
			require.NoError(t, env.GetWorkflowResult(&result))
			require.Equal(t, []any{}, result.Output)
		}
	}
}
func TestForEachFailsAndDoesNotRunNextNode(t *testing.T) {
	d := loopDefinition()
	d.Steps[0].Branches[0].Steps = []model.Step{{ID: "fail", Type: "fail", Message: "Invalid invoice"}}
	d.Steps = append(d.Steps, model.Step{ID: "never", Type: "activity", Activity: "must.not.run"})
	d, err := definition.Normalize(d)
	require.NoError(t, err)
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.ExecuteWorkflow(engine.RunloomWorkflow, model.RunInput{Definition: d, Input: map[string]any{"items": []any{1, 2, 3}}})
	require.ErrorContains(t, env.GetWorkflowError(), "Invalid invoice")
}
func TestForEachCancellationDuringWait(t *testing.T) {
	d := loopDefinition()
	d.Steps[0].Branches[0].Steps = []model.Step{{ID: "wait", Type: "wait", Seconds: 3600}}
	d.Steps[0].ForEach.Output = "${loop.item}"
	d, err := definition.Normalize(d)
	require.NoError(t, err)
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterDelayedCallback(func() { env.CancelWorkflow() }, time.Second)
	env.ExecuteWorkflow(engine.RunloomWorkflow, model.RunInput{Definition: d, Input: map[string]any{"items": []any{1, 2, 3}}})
	require.Error(t, env.GetWorkflowError())
}
func TestForEachActivitiesUseIterationInputs(t *testing.T) {
	d := loopDefinition()
	d.Steps[0].Branches[0].Steps = []model.Step{{ID: "call", Type: "activity", Activity: "runloom.echo", With: map[string]any{"value": "${loop.item}"}}}
	d.Steps[0].ForEach.Output = "${steps.call.output}"
	d, err := definition.Normalize(d)
	require.NoError(t, err)
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(func(_ context.Context, input map[string]any) (map[string]any, error) { return input, nil }, activity.RegisterOptions{Name: "runloom.echo"})
	env.ExecuteWorkflow(engine.RunloomWorkflow, model.RunInput{Definition: d, Input: map[string]any{"items": []any{"a", "b"}}})
	require.NoError(t, env.GetWorkflowError())
	var result model.RunResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, []any{map[string]any{"value": "a"}, map[string]any{"value": "b"}}, result.Output)
}
func TestForEachRejectsEndAndNestedLoops(t *testing.T) {
	for _, child := range []model.Step{{ID: "end", Type: "end"}, loopDefinition().Steps[0]} {
		d := loopDefinition()
		child.ID = "nested"
		d.Steps[0].Branches[0].Steps = []model.Step{child}
		_, err := definition.Normalize(d)
		require.Error(t, err)
		require.NotEmpty(t, definition.NodeIssues(d, nil))
	}
}

func TestForEachCallbacksAreIsolatedAcrossIterations(t *testing.T) {
	d := loopDefinition()
	d.Steps[0].ForEach.Concurrency = 1
	d.Steps[0].ForEach.Output = "${steps.reply.output}"
	d.Steps[0].Branches[0].Steps = []model.Step{{ID: "reply", Type: "callback", CallbackKey: "ready", Prompt: "Wait", OnTimeout: "fail"}}
	d, err := definition.Normalize(d)
	require.NoError(t, err)
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	state := func() model.RunState {
		raw, err := env.QueryWorkflow("status")
		require.NoError(t, err)
		var result model.RunState
		require.NoError(t, raw.Get(&result))
		return result
	}
	var first string
	env.RegisterDelayedCallback(func() {
		s := state()
		require.Len(t, s.WaitingCallbacks, 1)
		for id := range s.WaitingCallbacks {
			first = id
			require.NotEqual(t, "reply", id)
			env.SignalWorkflow("callback", model.WorkflowEvent{StepID: id, Payload: "first"})
		}
	}, time.Second)
	env.RegisterDelayedCallback(func() {
		s := state()
		require.Len(t, s.WaitingCallbacks, 1)
		for id := range s.WaitingCallbacks {
			require.NotEqual(t, first, id)
		}
		env.SignalWorkflow("callback", model.WorkflowEvent{StepID: first, Payload: "stale"})
	}, 2*time.Second)
	env.RegisterDelayedCallback(func() {
		s := state()
		require.Len(t, s.WaitingCallbacks, 1, "stale callback must not release the next iteration")
		for id := range s.WaitingCallbacks {
			env.SignalWorkflow("callback", model.WorkflowEvent{StepID: id, Payload: "second"})
		}
	}, 3*time.Second)
	env.ExecuteWorkflow(engine.RunloomWorkflow, model.RunInput{Definition: d, Input: map[string]any{"items": []any{1, 2}}})
	require.NoError(t, env.GetWorkflowError())
}
