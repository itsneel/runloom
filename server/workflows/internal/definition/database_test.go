package definition

import (
	"encoding/json"
	"github.com/example/runloom/workflows/internal/dbnode"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestDatabaseContractAndMappings(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/spec/workflow.schema.json")
	require.NoError(t, err)
	var contract map[string]any
	require.NoError(t, json.Unmarshal(raw, &contract))
	schema, err := compileInputSchema(contract)
	require.NoError(t, err)
	value := map[string]any{"schema_version": "1.0", "id": "database", "name": "Database", "steps": []any{map[string]any{"id": "read", "type": "database", "database": map[string]any{"connection_id": "reporting", "operation": "sql", "query": "SELECT $1", "parameters": []any{"${input.name}"}}}}}
	require.NoError(t, schema.Validate(value))
	raw, err = json.Marshal(value)
	require.NoError(t, err)
	var d model.Definition
	require.NoError(t, json.Unmarshal(raw, &d))
	_, err = Normalize(d)
	require.NoError(t, err)
	d.Steps[0].Retry = &model.RetryPolicy{MaximumAttempts: 2}
	_, err = Normalize(d)
	require.Error(t, err)
	op := dbnode.Operation{ConnectionID: "reporting", Operation: "insert", Table: "customers", Values: map[string]any{"name": "${input.name}", "active": "${input.active}"}}
	resolved, err := ResolveDatabase(op, map[string]any{"input": map[string]any{"name": "${input.inject}", "active": true}})
	require.NoError(t, err)
	require.Equal(t, true, resolved.Values["active"])
	require.Equal(t, "${input.inject}", resolved.Values["name"])
	require.Equal(t, "${input.name}", op.Values["name"])
	_, err = ResolveDatabase(op, map[string]any{})
	require.Error(t, err)
	value["steps"].([]any)[0].(map[string]any)["retry"] = map[string]any{"maximum_attempts": 2}
	require.Error(t, schema.Validate(value))
}
