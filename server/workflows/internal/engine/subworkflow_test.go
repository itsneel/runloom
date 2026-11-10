package engine

import (
	"context"
	"testing"
	"time"

	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

func TestSubworkflowExecution(t *testing.T) {
	for _, mode := range []string{"mapped-output", "cancel", "timeout", "failure", "missing-snapshot", "empty-input", "end"} {
		t.Run(mode, func(t *testing.T) {
			suite := testsuite.WorkflowTestSuite{}
			env := suite.NewTestWorkflowEnvironment()
			registerActivities(env)
			registered := 0
			env.RegisterActivityWithOptions(func(ctx context.Context, input model.RunInput) error {
				registered++
				require.Equal(t, "child", input.Source)
				require.NotEmpty(t, input.ParentWorkflowID)
				require.Nil(t, input.Definitions)
				require.Empty(t, input.Definition.Steps)
				return nil
			}, activity.RegisterOptions{Name: "runloom.internal.record_run"})
			child := model.Definition{SchemaVersion: "1.0", ID: "child", Name: "Child", Version: 1, Steps: []model.Step{{ID: "echo", Type: "activity", Activity: "runloom.echo", With: map[string]any{"value": "${input.value}"}}}, Output: "${steps.echo.output}"}
			child.Kind = "child"
			if mode == "end" {
				child.Steps = []model.Step{{ID: "done", Type: "end", With: map[string]any{"value": "${input.value}"}}}
			}
			if mode == "cancel" || mode == "timeout" {
				child.Steps = []model.Step{{ID: "wait", Type: "wait", Seconds: 3600}}
			}
			if mode == "failure" {
				child.Output = "${input.missing}"
			}
			if mode == "empty-input" {
				child.Steps[0].With = map[string]any{"value": "default"}
			}
			var err error
			child, err = definition.Normalize(child)
			require.NoError(t, err)
			parent, err := definition.Normalize(model.Definition{SchemaVersion: "1.0", ID: "parent", Name: "Parent", Version: 1, Steps: []model.Step{{ID: "call", Type: "subworkflow", DefinitionID: "child", Version: 1, With: map[string]any{"value": "${input.message}"}}}, Output: "${steps.call.output}"})
			require.NoError(t, err)
			deps := map[string]model.Definition{"child@1": child}
			if mode == "timeout" {
				timeout := 1
				parent.Steps[0].TimeoutSeconds = &timeout
			}
			if mode == "missing-snapshot" {
				deps = nil
			}
			if mode == "empty-input" {
				parent.Steps[0].With = nil
			}
			if mode == "cancel" {
				env.RegisterDelayedCallback(env.CancelWorkflow, time.Second)
			}
			env.ExecuteWorkflow(RunloomWorkflow, model.RunInput{Definition: parent, Definitions: deps, Input: map[string]any{"message": "hello"}, TeamID: "team"})
			switch mode {
			case "mapped-output", "empty-input", "end":
				require.NoError(t, env.GetWorkflowError())
				var result model.RunResult
				require.NoError(t, env.GetWorkflowResult(&result))
				expected := "hello"
				if mode == "empty-input" {
					expected = "default"
				}
				require.Equal(t, expected, result.Output.(map[string]any)["value"])
				state, err := env.QueryWorkflow("status")
				require.NoError(t, err)
				var decoded model.RunState
				require.NoError(t, state.Get(&decoded))
				require.Contains(t, decoded.ChildWorkflows, "call")
			case "cancel":
				require.True(t, temporal.IsCanceledError(env.GetWorkflowError()))
			default:
				require.Error(t, env.GetWorkflowError())
			}
			if mode != "missing-snapshot" {
				require.Equal(t, 1, registered)
			}
		})
	}
}
