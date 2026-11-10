package definition

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/example/runloom/workflows/internal/mailnode"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
)

func TestEmailSampleAndValidation(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/spec/workflow.schema.json")
	require.NoError(t, err)
	var contract map[string]any
	require.NoError(t, json.Unmarshal(raw, &contract))
	schema, err := compileInputSchema(contract)
	require.NoError(t, err)
	raw, err = os.ReadFile("../../testdata/examples/email-notification.json")
	require.NoError(t, err)
	var value map[string]any
	require.NoError(t, json.Unmarshal(raw, &value))
	require.NoError(t, schema.Validate(value))
	var d model.Definition
	require.NoError(t, json.Unmarshal(raw, &d))
	_, err = Normalize(d)
	require.NoError(t, err)
	step := value["steps"].([]any)[0].(map[string]any)
	step["retry"] = map[string]any{"maximum_attempts": 2}
	require.Error(t, schema.Validate(value))
	delete(step, "retry")
	d.Steps[0].Retry = &model.RetryPolicy{MaximumAttempts: 2}
	_, err = Normalize(d)
	require.Error(t, err)
	d.Steps[0].Retry = nil
	resolved, err := ResolveEmail(*d.Steps[0].Email, map[string]any{"input": map[string]any{"email": "to@example.test", "name": "Runloom"}})
	require.NoError(t, err)
	require.NoError(t, mailnode.Validate(&resolved, false))
	require.Equal(t, "Hello Runloom, your workflow has completed.", resolved.Text)
	require.Equal(t, []string{"${input.email}"}, d.Steps[0].Email.To, "mapping must not mutate definition")
	resolved, err = ResolveEmail(*d.Steps[0].Email, map[string]any{"input": map[string]any{"email": "bad\r\nBcc: stolen@example.test", "name": "Runloom"}})
	require.NoError(t, err)
	require.Error(t, mailnode.Validate(&resolved, false))
	_, err = ResolveEmail(*d.Steps[0].Email, map[string]any{"input": map[string]any{"email": 42, "name": "Runloom"}})
	require.Error(t, err)
}
