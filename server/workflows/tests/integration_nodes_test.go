package workflows_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/engine"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestIntegrationCatalogBuildsFixedRequests(t *testing.T) {
	catalog := definition.IntegrationCatalog()
	require.Len(t, catalog, 10)
	for _, op := range catalog {
		t.Run(op.ID, func(t *testing.T) {
			params := map[string]any{}
			for _, p := range op.Parameters {
				if p.Type == "array" {
					params[p.Name] = []any{[]any{"value", float64(2)}}
				} else {
					params[p.Name] = "sample"
				}
			}
			in := model.Integration{Operation: op.ID, Secret: "integration-token", Parameters: params}
			request, err := definition.BuildIntegration(in)
			require.NoError(t, err)
			u, err := url.Parse(request.URL)
			require.NoError(t, err)
			require.Equal(t, "https", u.Scheme)
			require.NotEmpty(t, u.Host)
			require.Equal(t, "integration-token", request.SecretHeaders["Authorization"].Secret)
			require.Equal(t, "Bearer ", request.SecretHeaders["Authorization"].Prefix)
			require.NotContains(t, request.URL, "integration-token")
			require.NoError(t, definition.ValidateHTTP(&request))
		})
	}
}
func TestIntegrationRejectsBadParametersAndEncodesIDs(t *testing.T) {
	for _, in := range []model.Integration{
		{Operation: "shell", Secret: "key"},
		{Operation: "slack-send", Secret: "Bearer actual-token", Parameters: map[string]any{"channel": "c", "text": "hello"}},
		{Operation: "slack-send", Secret: "key", Parameters: map[string]any{"channel": "c", "text": "hello", "url": "https://attacker.example"}},
		{Operation: "slack-send", Secret: "key", Parameters: map[string]any{"channel": "c"}},
		{Operation: "sheets-append", Secret: "key", Parameters: map[string]any{"spreadsheet": "id", "range": "A1", "values": []any{"not a row"}}},
	} {
		_, err := definition.BuildIntegration(in)
		require.Error(t, err)
	}
	request, err := definition.BuildIntegration(model.Integration{Operation: "sheets-read", Secret: "key", Parameters: map[string]any{"spreadsheet": "id", "range": "Sheet 1!A1:B2?foo=bar"}})
	require.NoError(t, err)
	u, err := url.Parse(request.URL)
	require.NoError(t, err)
	require.Empty(t, u.Query().Get("foo"))
	require.Equal(t, "sheets.googleapis.com", u.Host)
}
func TestIntegrationWorkflowUsesTeamSecretAndDetectsSlackErrors(t *testing.T) {
	for _, ok := range []bool{true, false} {
		var suite testsuite.WorkflowTestSuite
		env := suite.NewTestWorkflowEnvironment()
		env.RegisterActivityWithOptions(func(_ context.Context, in model.IntegrationInput) (map[string]any, error) {
			require.Equal(t, "team-a", in.TeamID)
			require.Equal(t, "slack-token", in.Integration.Secret)
			require.Equal(t, "hello", in.Integration.Parameters["text"])
			output := map[string]any{"status_code": 200, "body": map[string]any{"ok": ok}}
			return output, definition.ValidateIntegrationResponse(in.Integration.Operation, output)
		}, activity.RegisterOptions{Name: "runloom.internal.integration"})
		d, err := definition.Normalize(model.Definition{ID: "integration", Name: "Integration", SchemaVersion: "1.0", Steps: []model.Step{{ID: "send", Type: "integration", Integration: &model.Integration{Operation: "slack-send", Secret: "slack-token", Parameters: map[string]any{"channel": "channel-a", "text": "${input.message}"}}}}, Output: "${steps.send.output}"})
		require.NoError(t, err)
		env.ExecuteWorkflow(engine.RunloomWorkflow, model.RunInput{TeamID: "team-a", Definition: d, Input: map[string]any{"message": "hello"}})
		if ok {
			require.NoError(t, env.GetWorkflowError())
		} else {
			require.ErrorContains(t, env.GetWorkflowError(), "Slack rejected")
		}
	}
}
