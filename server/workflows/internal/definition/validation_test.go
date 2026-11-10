package definition

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
)

func loadExample(t *testing.T) model.Definition {
	t.Helper()
	data, err := os.ReadFile("../../testdata/examples/employee-onboarding.json")
	require.NoError(t, err)
	var value model.Definition
	require.NoError(t, json.Unmarshal(data, &value))
	return value
}

func TestNormalizeCompleteExample(t *testing.T) {
	value, err := Normalize(loadExample(t))

	require.NoError(t, err)
	require.Equal(t, 2.0, value.Steps[0].Retry.BackoffCoefficient)
	require.Equal(t, 60, *value.Steps[2].Then[0].Branches[0].Steps[0].TimeoutSeconds)
}

func TestNormalizeRejectsNestedDuplicate(t *testing.T) {
	value := loadExample(t)
	value.Steps[2].Else[0].ID = "email-account"

	_, err := Normalize(value)
	require.ErrorContains(t, err, "duplicate step id")
}

func TestNormalizeRejectsRemoteReference(t *testing.T) {
	value := loadExample(t)
	value.InputSchema = map[string]any{"$ref": "https://attacker.invalid/schema.json"}

	_, err := Normalize(value)
	require.ErrorContains(t, err, "remote")
}

func TestValidateInput(t *testing.T) {
	value, err := Normalize(loadExample(t))
	require.NoError(t, err)

	err = ValidateInput(value.InputSchema, map[string]any{"department": "Engineering"})
	require.ErrorContains(t, err, "employee_name")
}

func TestNormalizeRejectsFieldsFromAnotherStepType(t *testing.T) {
	value := loadExample(t)
	value.Steps[3].Activity = "runloom.echo"

	_, err := Normalize(value)
	require.ErrorContains(t, err, "unsupported by wait")
}

func TestNormalizeCallbackAndPauseRules(t *testing.T) {
	value := loadExample(t)
	value.Steps = append(value.Steps,
		model.Step{ID: "external-result", Type: "callback", CallbackKey: "provisioning.ready", Prompt: "Wait for provisioning"},
		model.Step{ID: "review-checkpoint", Type: "pause", Message: "Review the result"},
	)

	normalized, err := Normalize(value)

	require.NoError(t, err)
	require.Equal(t, "fail", normalized.Steps[len(normalized.Steps)-2].OnTimeout)
	require.Equal(t, "fail", normalized.Steps[len(normalized.Steps)-1].OnTimeout)
}

func TestNormalizeRejectsCallbackWithoutContractKey(t *testing.T) {
	value := loadExample(t)
	value.Steps = append(value.Steps, model.Step{ID: "external-result", Type: "callback", Prompt: "Wait"})

	_, err := Normalize(value)

	require.ErrorContains(t, err, "callback_key")
}

func TestNormalizeWebhookLLMAndNamedSignal(t *testing.T) {
	value := loadExample(t)
	value.Trigger = model.Trigger{Type: "webhook", Path: "employee-created", Method: "POST"}
	value.Steps = append(value.Steps,
		model.Step{ID: "draft-message", Type: "llm", Model: "default", SystemPrompt: "Write concise messages.", Prompt: "Welcome ${input.employee_name}"},
		model.Step{ID: "wait-for-payroll", Type: "signal", SignalName: "payroll.ready", Prompt: "Wait for payroll"},
	)

	normalized, err := Normalize(value)

	require.NoError(t, err)
	require.Equal(t, "employee-created", normalized.Trigger.Path)
	require.Equal(t, 120, *normalized.Steps[len(normalized.Steps)-2].TimeoutSeconds)
	require.Equal(t, 3, normalized.Steps[len(normalized.Steps)-2].Retry.MaximumAttempts)
	require.Equal(t, "fail", normalized.Steps[len(normalized.Steps)-1].OnTimeout)
}

func TestNormalizeRejectsDuplicateSignalNames(t *testing.T) {
	value := loadExample(t)
	value.Steps = append(value.Steps,
		model.Step{ID: "signal-a", Type: "signal", SignalName: "ready", Prompt: "A"},
		model.Step{ID: "signal-b", Type: "signal", SignalName: "ready", Prompt: "B"},
	)

	_, err := Normalize(value)

	require.ErrorContains(t, err, "duplicate signal name")
}
