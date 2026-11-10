package engine

import (
	"context"
	"testing"

	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func TestEmailMappingAndSingleAttempt(t *testing.T) {
	for _, fail := range []bool{false, true} {
		suite := testsuite.WorkflowTestSuite{}
		env := suite.NewTestWorkflowEnvironment()
		calls := 0
		env.RegisterActivityWithOptions(func(_ context.Context, input model.EmailActivityInput) (map[string]any, error) {
			calls++
			require.Equal(t, "team-a", input.TeamID)
			require.Equal(t, []string{"local@example.test"}, input.Message.To)
			require.Equal(t, "Hi Runloom", input.Message.Text)
			if fail {
				return nil, temporal.NewApplicationError("SMTP acknowledgement missing", "SMTP")
			}
			return map[string]any{"accepted": true}, nil
		}, activity.RegisterOptions{Name: "runloom.internal.email"})
		d, err := definition.Normalize(model.Definition{SchemaVersion: "1.0", ID: "email-test", Name: "Email", Steps: []model.Step{{ID: "send", Type: "email", Email: &model.EmailMessage{To: []string{"${input.email}"}, Subject: "Test", Text: "Hi ${input.name}"}}}})
		require.NoError(t, err)
		env.ExecuteWorkflow(RunloomWorkflow, model.RunInput{TeamID: "team-a", Definition: d, Input: map[string]any{"email": "local@example.test", "name": "Runloom"}})
		if fail {
			require.Error(t, env.GetWorkflowError())
		} else {
			require.NoError(t, env.GetWorkflowError())
		}
		require.Equal(t, 1, calls)
	}
}

func TestLLMMetadataVersionGate(t *testing.T) {
	for _, version := range []workflow.Version{workflow.DefaultVersion, 1} {
		for _, kind := range []string{"llm", "activity"} {
			suite := testsuite.WorkflowTestSuite{}
			env := suite.NewTestWorkflowEnvironment()
			env.OnGetVersion("llm-usage-metadata-v1", workflow.DefaultVersion, workflow.Version(1)).Return(version)
			env.RegisterActivityWithOptions(func(_ context.Context, input map[string]any) (map[string]any, error) {
				if version == workflow.DefaultVersion {
					require.NotContains(t, input, "_runloom_step_id")
				} else {
					require.Equal(t, "chat", input["_runloom_step_id"])
				}
				return map[string]any{"content": "done"}, nil
			}, activity.RegisterOptions{Name: "runloom.llm"})
			step := model.Step{ID: "chat", Type: kind, With: map[string]any{"prompt": "test"}}
			if kind == "llm" {
				step.With = nil
				step.Prompt = "test"
				step.Model = "test-model"
			}
			if kind == "activity" {
				step.Activity = "runloom.llm"
			}
			d, err := definition.Normalize(model.Definition{SchemaVersion: "1.0", ID: "llm-test", Name: "LLM", Steps: []model.Step{step}})
			require.NoError(t, err)
			env.ExecuteWorkflow(RunloomWorkflow, model.RunInput{Definition: d})
			require.NoError(t, env.GetWorkflowError())
		}
	}
}
