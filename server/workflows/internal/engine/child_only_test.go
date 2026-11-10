package engine

import (
	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
	"testing"
)

func TestChildRequiresActualTemporalParent(t *testing.T) {
	for _, source := range []string{"", "schedule", "child", "child_test"} {
		t.Run("source-"+source, func(t *testing.T) {
			suite := testsuite.WorkflowTestSuite{}
			env := suite.NewTestWorkflowEnvironment()
			// No activities registered: no business or registration activity may run.
			env.ExecuteWorkflow(RunloomWorkflow, model.RunInput{Definition: model.Definition{Kind: "child"}, Source: source, IsTest: true, ParentWorkflowID: "spoofed-parent"})
			require.ErrorContains(t, env.GetWorkflowError(), "ChildOnlyWorkflow")
		})
	}
}
