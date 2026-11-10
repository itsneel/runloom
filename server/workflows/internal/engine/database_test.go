package engine

import (
	"context"
	"github.com/example/runloom/workflows/internal/dbnode"
	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"testing"
)

func TestDatabaseMappingAndSingleAttempt(t *testing.T) {
	for _, fail := range []bool{false, true} {
		suite := testsuite.WorkflowTestSuite{}
		env := suite.NewTestWorkflowEnvironment()
		calls := 0
		env.RegisterActivityWithOptions(func(_ context.Context, in dbnode.Input) (any, error) {
			calls++
			require.Equal(t, "team-a", in.TeamID)
			require.Equal(t, "orders", in.Operation.ConnectionID)
			require.Equal(t, []any{"x' OR true --"}, in.Operation.Parameters)
			require.Equal(t, "SELECT * FROM customers WHERE name=$1", in.Operation.Query)
			if fail {
				return nil, temporal.NewApplicationError("database unavailable", "Database")
			}
			return map[string]any{"rows": []any{map[string]any{"name": "test"}}, "affected": 0, "truncated": false}, nil
		}, activity.RegisterOptions{Name: "runloom.internal.database"})
		d, err := definition.Normalize(model.Definition{SchemaVersion: "1.0", ID: "database-test", Name: "Database", Steps: []model.Step{{ID: "read", Type: "database", Database: &dbnode.Operation{ConnectionID: "orders", Operation: "sql", Query: "SELECT * FROM customers WHERE name=$1", Parameters: []any{"${input.name}"}}}}})
		require.NoError(t, err)
		env.ExecuteWorkflow(RunloomWorkflow, model.RunInput{TeamID: "team-a", Definition: d, Input: map[string]any{"name": "x' OR true --"}})
		if fail {
			require.Error(t, env.GetWorkflowError())
		} else {
			require.NoError(t, env.GetWorkflowError())
		}
		require.Equal(t, 1, calls)
	}
}
