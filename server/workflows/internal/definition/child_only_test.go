package definition

import (
	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestWorkflowKinds(t *testing.T) {
	value := model.Definition{SchemaVersion: "1.0", ID: "example", Name: "Example", Steps: []model.Step{{ID: "echo", Type: "activity", Activity: "runloom.echo"}}}
	normalized, err := Normalize(value)
	require.NoError(t, err)
	require.Equal(t, "main", normalized.Kind)
	value.Kind = "invalid"
	_, err = Normalize(value)
	require.ErrorContains(t, err, "kind must be")
	value.Kind = "child"
	value.Trigger = model.Trigger{Type: "webhook", Path: "start", Method: "POST"}
	_, err = Normalize(value)
	require.ErrorContains(t, err, "cannot have webhook")
	value.Kind = "main"
	normalized, err = Normalize(value)
	require.NoError(t, err)
	parent := dependencyDefinition("parent", model.Step{ID: "call", Type: "subworkflow", DefinitionID: value.ID, Version: 1})
	_, err = ResolveDependencies(parent, func(string, *int) (model.Definition, error) { return normalized, nil })
	require.ErrorContains(t, err, "must be published with kind child")
}
