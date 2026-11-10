package engine

import (
	"context"
	"encoding/json"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestGreetingSampleWorkflows(t *testing.T) {
	read := func(path string, target any) {
		t.Helper()
		raw, err := os.ReadFile("../../testdata/examples/" + path + ".json")
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, target))
	}
	load := func(id string) model.Definition {
		t.Helper()
		var value model.Definition
		read(id, &value)
		value, err := definition.Normalize(value)
		require.NoError(t, err)
		value.Version = 1
		return value
	}
	for _, tc := range []struct {
		id, input, child string
		children         int
		output           map[string]any
	}{
		{"parent-with-subworkflow", "greeting", "reusable-greeting", 1, map[string]any{"message": "Hello, Avery!"}},
		{"parent-with-two-subworkflows", "customer-and-reviewer", "reusable-greeting", 2, map[string]any{"customer": "Hello, Avery!", "reviewer": "Hello, Jordan!"}},
		{"parent-with-parallel-subworkflows", "customer-and-reviewer", "parallel-greeting", 2, map[string]any{"customer": "Hello, Avery!", "reviewer": "Hello, Jordan!"}},
	} {
		t.Run(tc.id, func(t *testing.T) {
			child := load(tc.child)
			var input map[string]any
			read("inputs/"+tc.input, &input)
			root := load(tc.id)
			require.NoError(t, definition.ValidateInput(root.InputSchema, input))
			deps, err := definition.ResolveDependencies(root, func(id string, v *int) (model.Definition, error) {
				require.Equal(t, child.ID, id)
				require.Equal(t, 1, *v)
				return child, nil
			})
			require.NoError(t, err)
			suite := testsuite.WorkflowTestSuite{}
			env := suite.NewTestWorkflowEnvironment()
			registerActivities(env)
			var childCount atomic.Int32
			env.RegisterActivityWithOptions(func(ctx context.Context, run model.RunInput) error {
				childCount.Add(1)
				require.Equal(t, "child", run.Source)
				require.Equal(t, 1, run.Definition.Version)
				return definition.ValidateInput(run.Definition.InputSchema, run.Input)
			}, activity.RegisterOptions{Name: "runloom.internal.record_run"})
			parallelObserved := false
			if tc.id == "parent-with-parallel-subworkflows" {
				env.RegisterDelayedCallback(func() {
					query, err := env.QueryWorkflow("status")
					require.NoError(t, err)
					var state model.RunState
					require.NoError(t, query.Get(&state))
					require.Len(t, state.ChildWorkflows, 2, "both child runs must start before either five-second timer ends")
					require.NotEqual(t, state.ChildWorkflows["greet-customer"], state.ChildWorkflows["greet-reviewer"])
					parallelObserved = true
				}, time.Second)
			}
			started := env.Now()
			env.ExecuteWorkflow(RunloomWorkflow, model.RunInput{Definition: root, Definitions: deps, Input: input, TeamID: "sample-team"})
			require.NoError(t, env.GetWorkflowError())
			var result model.RunResult
			require.NoError(t, env.GetWorkflowResult(&result))
			require.Equal(t, tc.output, result.Output)
			require.EqualValues(t, tc.children, childCount.Load())
			if tc.id == "parent-with-parallel-subworkflows" {
				require.True(t, parallelObserved)
				require.Less(t, env.Now().Sub(started), 9*time.Second, "parallel timers must overlap rather than take ten seconds")
			}
		})
	}
}
