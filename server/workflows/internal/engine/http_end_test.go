package engine

import (
	"context"
	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"testing"
)

func TestHTTPAndEarlyEnd(t *testing.T) {
	suite := testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(func(_ context.Context, input model.HTTPActivityInput) (map[string]any, error) {
		require.Equal(t, "team-a", input.TeamID)
		require.Equal(t, "https://api.example.com/items/42", input.Request.URL)
		require.Equal(t, "key-name", input.Request.SecretHeaders["Authorization"].Secret)
		return map[string]any{"status_code": 200, "body": map[string]any{"ok": true}}, nil
	}, activity.RegisterOptions{Name: "runloom.internal.http"})
	d := model.Definition{SchemaVersion: "1.0", ID: "api-end", Name: "API end", Steps: []model.Step{
		{ID: "call", Type: "http", HTTP: &model.HTTPRequest{URL: "https://api.example.com/items/${input.id}", Method: "POST", Body: map[string]any{"id": "${input.id}"}, SecretHeaders: map[string]model.SecretHeader{"Authorization": {Secret: "key-name"}}}},
		{ID: "decision", Type: "condition", If: &model.ConditionExpression{Left: true, Operator: "equals", Right: true}, Then: []model.Step{{ID: "end", Type: "end", With: map[string]any{"result": "${steps.call.output.body.ok}"}}}},
		{ID: "must-not-run", Type: "activity", Activity: "unregistered.must-not-run"},
	}, Output: "${steps.must-not-run.output}"}
	d, err := definition.Normalize(d)
	require.NoError(t, err)
	env.ExecuteWorkflow(RunloomWorkflow, model.RunInput{TeamID: "team-a", Definition: d, Input: map[string]any{"id": "42"}})
	require.NoError(t, env.GetWorkflowError())
	var result model.RunResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, map[string]any{"result": true}, result.Output)
	require.NotContains(t, result.Steps, "must-not-run")
	require.Contains(t, result.Steps, "end")
}

func TestEndWithoutExplicitOutput(t *testing.T) {
	suite := testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	env.ExecuteWorkflow(RunloomWorkflow, model.RunInput{Definition: model.Definition{Steps: []model.Step{{ID: "end", Type: "end"}}, Output: "${input.missing}"}})
	require.NoError(t, env.GetWorkflowError())
}
