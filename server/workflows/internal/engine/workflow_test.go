package engine

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/example/runloom/workflows/internal/activities"
	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

func workflowExample(t *testing.T) model.Definition {
	t.Helper()
	data, err := os.ReadFile("../../testdata/examples/employee-onboarding.json")
	require.NoError(t, err)
	var value model.Definition
	require.NoError(t, json.Unmarshal(data, &value))
	value, err = definition.Normalize(value)
	require.NoError(t, err)
	value.Version = 1
	return value
}

func registerActivities(environment *testsuite.TestWorkflowEnvironment) {
	environment.RegisterActivityWithOptions(activities.Echo, activity.RegisterOptions{Name: "runloom.echo"})
	environment.RegisterActivityWithOptions(activities.Compose, activity.RegisterOptions{Name: "runloom.compose"})
	environment.RegisterActivityWithOptions(activities.Sum, activity.RegisterOptions{Name: "runloom.sum"})
}

func TestRunloomWorkflowApprovalParallelAndTimer(t *testing.T) {
	suite := testsuite.WorkflowTestSuite{}
	environment := suite.NewTestWorkflowEnvironment()
	registerActivities(environment)
	environment.RegisterDelayedCallback(func() {
		query, err := environment.QueryWorkflow("status")
		require.NoError(t, err)
		var state model.RunState
		require.NoError(t, query.Get(&state))
		require.Equal(t, "running", state.Status)
		environment.SignalWorkflow("approval", model.ApprovalDecision{StepID: "manager-approval", Approved: true, Comment: "approved", Actor: "manager"})
		environment.SignalWorkflow("callback", model.WorkflowEvent{StepID: "provisioning-callback", Payload: map[string]any{"status": "ready"}, Actor: "provisioning-service"})
		environment.SignalWorkflow("resume", model.WorkflowEvent{StepID: "operator-review", Payload: map[string]any{}, Actor: "operator"})
	}, time.Second)

	environment.ExecuteWorkflow(RunloomWorkflow, model.RunInput{Definition: workflowExample(t), Input: map[string]any{"employee_name": "Avery", "department": "Engineering"}})

	require.True(t, environment.IsWorkflowCompleted())
	require.NoError(t, environment.GetWorkflowError())
	var result model.RunResult
	require.NoError(t, environment.GetWorkflowResult(&result))
	require.Equal(t, "completed", result.Status)
	require.Equal(t, "email", result.Steps["email-account"].Output.(map[string]any)["system"])
	require.Equal(t, "chat", result.Steps["chat-account"].Output.(map[string]any)["system"])
	require.Equal(t, "ready", result.Output.(map[string]any)["provisioning"].(map[string]any)["status"])
}

func TestRunloomWorkflowCancellationIsNotSwallowed(t *testing.T) {
	suite := testsuite.WorkflowTestSuite{}
	environment := suite.NewTestWorkflowEnvironment()
	definitionValue := model.Definition{SchemaVersion: "1.0", ID: "cancel-test", Name: "Cancel test", Version: 1, InputSchema: map[string]any{}, Steps: []model.Step{{ID: "long-wait", Type: "wait", Seconds: 3600}}}
	environment.RegisterDelayedCallback(environment.CancelWorkflow, time.Second)

	environment.ExecuteWorkflow(RunloomWorkflow, model.RunInput{Definition: definitionValue, Input: map[string]any{}})

	require.Error(t, environment.GetWorkflowError())
	require.True(t, temporal.IsCanceledError(environment.GetWorkflowError()))
}

func TestRunloomWorkflowCallbackAndPauseResume(t *testing.T) {
	suite := testsuite.WorkflowTestSuite{}
	environment := suite.NewTestWorkflowEnvironment()
	definitionValue := model.Definition{
		SchemaVersion: "1.0",
		ID:            "interactive-test",
		Name:          "Interactive test",
		Version:       1,
		InputSchema:   map[string]any{},
		Steps: []model.Step{
			{ID: "external-result", Type: "callback", CallbackKey: "external.ready", Prompt: "Wait for external data", OnTimeout: "fail"},
			{ID: "operator-checkpoint", Type: "pause", Message: "Review before continuing", OnTimeout: "fail"},
			{ID: "named-event", Type: "signal", SignalName: "documents.ready", Prompt: "Wait for documents", OnTimeout: "fail"},
		},
		Output: map[string]any{
			"callback":   "${steps.external-result.output.payload}",
			"resumed_by": "${steps.operator-checkpoint.output.actor}",
			"signal":     "${steps.named-event.output.payload}",
		},
	}
	environment.RegisterDelayedCallback(func() {
		environment.SignalWorkflow("callback", model.WorkflowEvent{StepID: "external-result", Payload: map[string]any{"ready": true}, Actor: "crm"})
		environment.SignalWorkflow("resume", model.WorkflowEvent{StepID: "operator-checkpoint", Payload: map[string]any{}, Actor: "operator"})
		environment.SignalWorkflow("signal", model.WorkflowEvent{SignalName: "documents.ready", Payload: map[string]any{"count": 3}, Actor: "document-service"})
	}, time.Second)

	environment.ExecuteWorkflow(RunloomWorkflow, model.RunInput{Definition: definitionValue, Input: map[string]any{}})

	require.NoError(t, environment.GetWorkflowError())
	var result model.RunResult
	require.NoError(t, environment.GetWorkflowResult(&result))
	output := result.Output.(map[string]any)
	require.Equal(t, true, output["callback"].(map[string]any)["ready"])
	require.Equal(t, "operator", output["resumed_by"])
	require.Equal(t, float64(3), output["signal"].(map[string]any)["count"])
}
