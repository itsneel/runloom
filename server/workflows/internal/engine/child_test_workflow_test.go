package engine

import (
	"context"
	"testing"

	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestIndependentChildTestAndNestedChildren(t *testing.T) {
	testIndependentTestAndNestedChildren(t, RunloomChildTestWorkflow, "child", 1)
}

func TestDraftTestsAndNestedChildren(t *testing.T) {
	for _, kind := range []string{"main", "child"} {
		t.Run(kind, func(t *testing.T) {
			testIndependentTestAndNestedChildren(t, RunloomDraftTestWorkflow, kind, 0)
		})
	}
}

func testIndependentTestAndNestedChildren(t *testing.T, entry any, kind string, version int) {
	child, err := definition.Normalize(model.Definition{SchemaVersion: "1.0", Kind: "child", ID: "leaf", Name: "Leaf", Version: 1, Steps: []model.Step{{ID: "echo", Type: "activity", Activity: "runloom.echo", With: map[string]any{"message": "${input.message}"}}}, Output: "${steps.echo.output}"})
	require.NoError(t, err)
	root, err := definition.Normalize(model.Definition{SchemaVersion: "1.0", Kind: "child", ID: "root", Name: "Root child", Version: 1, Steps: []model.Step{{ID: "call", Type: "subworkflow", DefinitionID: "leaf", Version: 1, With: map[string]any{"message": "${input.message}"}}}, Output: "${steps.call.output}"})
	require.NoError(t, err)
	root.Kind, root.Version = kind, version
	suite := testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(RunloomWorkflow)
	registerActivities(env)
	registered := 0
	env.RegisterActivityWithOptions(func(ctx context.Context, input model.RunInput) error {
		registered++
		require.True(t, input.IsTest)
		require.Equal(t, "child", input.Source)
		return nil
	}, activity.RegisterOptions{Name: "runloom.internal.record_run"})
	env.ExecuteWorkflow(entry, model.RunInput{Definition: root, Definitions: map[string]model.Definition{"leaf@1": child}, Input: map[string]any{"message": "test"}, TeamID: "team"})
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 1, registered)
	var result model.RunResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, map[string]any{"message": "test"}, result.Output)
	query, err := env.QueryWorkflow("status")
	require.NoError(t, err)
	var state model.RunState
	require.NoError(t, query.Get(&state))
	require.True(t, state.IsTest)
	require.Empty(t, state.ParentWorkflowID)
}

func TestChildTestEntryRejectsMainAndDraft(t *testing.T) {
	for _, value := range []model.Definition{{Kind: "main", Version: 1}, {Kind: "child", Version: 0}} {
		suite := testsuite.WorkflowTestSuite{}
		env := suite.NewTestWorkflowEnvironment()
		env.ExecuteWorkflow(RunloomChildTestWorkflow, model.RunInput{Definition: value})
		require.ErrorContains(t, env.GetWorkflowError(), "InvalidChildTest")
	}
}
