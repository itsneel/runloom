package definition

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
)

func TestDecisionExampleSchemaAndRuntime(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/spec/workflow.schema.json")
	require.NoError(t, err)
	var contract map[string]any
	require.NoError(t, json.Unmarshal(raw, &contract))
	schema, err := compileInputSchema(contract)
	require.NoError(t, err)
	raw, err = os.ReadFile("../../testdata/examples/ai-decision-modes.json")
	require.NoError(t, err)
	var value any
	require.NoError(t, json.Unmarshal(raw, &value))
	require.NoError(t, schema.Validate(value))
	var d model.Definition
	require.NoError(t, json.Unmarshal(raw, &d))
	normalized, err := Normalize(d)
	require.NoError(t, err)
	raw, err = json.Marshal(normalized)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &value))
	require.NoError(t, schema.Validate(value))
}

func decisionDefinition() model.Definition {
	return model.Definition{SchemaVersion: "1.0", ID: "decision-test", Name: "Decision", Steps: []model.Step{{ID: "route", Type: "decision", Model: "default", Prompt: "Choose", Decision: &model.DecisionConfig{Mode: "agent"}, Branches: []model.Branch{{ID: "a", Description: "Action A", Steps: []model.Step{{ID: "echo", Type: "activity", Activity: "runloom.echo"}}}}}}}
}
func TestDecisionDefinition(t *testing.T) {
	d, err := Normalize(decisionDefinition())
	require.NoError(t, err)
	require.Equal(t, 5, d.Steps[0].Decision.MaxRounds)
	require.Equal(t, 1, d.Steps[0].Retry.MaximumAttempts)
	require.NotNil(t, d.Steps[0].Branches[0].Steps[0].Retry)
	for _, mutate := range []func(*model.Step){
		func(s *model.Step) { s.Decision = nil },
		func(s *model.Step) { s.Decision.Mode = "unknown" },
		func(s *model.Step) { s.Decision.MaxRounds = 21 },
		func(s *model.Step) { s.Decision.MaxActions = 51 },
		func(s *model.Step) { s.Decision.Mode = "single"; s.Decision.MaxRounds = 2 },
		func(s *model.Step) { s.Decision.Execution = "parallel" },
		func(s *model.Step) { s.Branches = nil },
		func(s *model.Step) { s.Branches[0].Description = " " },
		func(s *model.Step) { s.Branches[0].Steps = nil },
		func(s *model.Step) { s.Branches = append(s.Branches, s.Branches[0]) },
		func(s *model.Step) { s.Branches[0].Steps[0].ID = s.ID },
		func(s *model.Step) { s.Branches[0].Steps = []model.Step{{ID: "end", Type: "end"}} },
		func(s *model.Step) {
			s.Branches[0].Steps = decisionDefinition().Steps
			s.Branches[0].Steps[0].ID = "nested"
		},
		func(s *model.Step) { s.Retry = &model.RetryPolicy{MaximumAttempts: 4} },
		func(s *model.Step) { s.With = map[string]any{"unsafe": true} },
	} {
		d := decisionDefinition()
		mutate(&d.Steps[0])
		_, err := Normalize(d)
		require.Error(t, err)
	}
}
func TestDecisionPinnedDependencies(t *testing.T) {
	d := decisionDefinition()
	d.Steps[0].Branches[0].Steps = []model.Step{{ID: "child-call", Type: "subworkflow", DefinitionID: "child", Version: 2}}
	calls := 0
	get := func(id string, version *int) (model.Definition, error) {
		calls++
		require.Equal(t, "child", id)
		require.Equal(t, 2, *version)
		return model.Definition{SchemaVersion: "1.0", ID: "child", Name: "Child", Kind: "child", Version: 2, Steps: []model.Step{{ID: "done", Type: "end"}}}, nil
	}
	deps, err := ResolveDependencies(d, get)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Contains(t, deps, "child@2")
}
