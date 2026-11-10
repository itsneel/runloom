package engine

import (
	"context"
	"github.com/example/runloom/workflows/internal/codecontract"
	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"testing"
)

func TestCodeActivityMappingAndOutput(t *testing.T) {
	suite := testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	source := "return { greeting: `Hello ${input.name}` };"
	env.RegisterActivityWithOptions(func(_ context.Context, input model.CodeActivityInput) (any, error) {
		require.Equal(t, "team-a", input.TeamID)
		require.Equal(t, source, input.Code.Source)
		require.Equal(t, "Runloom", input.Input["name"])
		return map[string]any{"greeting": "Hello Runloom"}, nil
	}, activity.RegisterOptions{Name: "runloom.internal.code"})
	d, err := definition.Normalize(model.Definition{SchemaVersion: "1.0", ID: "code", Name: "Code", Steps: []model.Step{
		{ID: "transform", Type: "code", Code: &model.JavaScriptCode{Runtime: codecontract.Runtime, Source: source}, With: map[string]any{"name": "${input.name}"}},
		{ID: "end", Type: "end", With: map[string]any{"message": "${steps.transform.output.greeting}"}},
	}})
	require.NoError(t, err)
	env.ExecuteWorkflow(RunloomWorkflow, model.RunInput{TeamID: "team-a", Definition: d, Input: map[string]any{"name": "Runloom"}})
	require.NoError(t, env.GetWorkflowError())
	var result model.RunResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, map[string]any{"message": "Hello Runloom"}, result.Output)
}
