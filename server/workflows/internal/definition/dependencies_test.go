package definition

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/example/runloom/workflows/internal/model"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
)

func TestSubworkflowExamplesMatchSchema(t *testing.T) {
	schemaRaw, err := os.ReadFile("../../testdata/spec/workflow.schema.json")
	require.NoError(t, err)
	var schemaValue any
	require.NoError(t, json.Unmarshal(schemaRaw, &schemaValue))
	compiler := jsonschema.NewCompiler()
	require.NoError(t, compiler.AddResource("urn:runloom:workflow", schemaValue))
	schema, err := compiler.Compile("urn:runloom:workflow")
	require.NoError(t, err)
	for _, name := range []string{"reusable-greeting", "parent-with-subworkflow", "parent-with-two-subworkflows", "parallel-greeting", "parent-with-parallel-subworkflows", "external-api"} {
		raw, err := os.ReadFile("../../testdata/examples/" + name + ".json")
		require.NoError(t, err)
		var value any
		require.NoError(t, json.Unmarshal(raw, &value))
		require.NoError(t, schema.Validate(value))
		var parsed model.Definition
		require.NoError(t, json.Unmarshal(raw, &parsed))
		_, err = Normalize(parsed)
		require.NoError(t, err)
	}
}

func dependencyDefinition(id string, steps ...model.Step) model.Definition {
	return model.Definition{Kind: "child", SchemaVersion: "1.0", ID: id, Name: id, Version: 1, Steps: steps}
}

func TestResolvePinnedDependencies(t *testing.T) {
	child := dependencyDefinition("child", model.Step{ID: "echo", Type: "activity", Activity: "runloom.echo"})
	root := dependencyDefinition("parent", model.Step{ID: "nested", Type: "parallel", Branches: []model.Branch{{ID: "branch", Steps: []model.Step{{ID: "call", Type: "subworkflow", DefinitionID: "child", Version: 1}}}, {ID: "other", Steps: []model.Step{{ID: "wait", Type: "wait", Seconds: 1}}}}})
	calls := 0
	get := func(id string, version *int) (model.Definition, error) {
		calls++
		require.Equal(t, "child", id)
		require.Equal(t, 1, *version)
		return child, nil
	}
	values, err := ResolveDependencies(root, get)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, child.ID, values["child@1"].ID)
	_, err = ResolveDependencies(root, func(string, *int) (model.Definition, error) { return model.Definition{}, fmt.Errorf("not found") })
	require.ErrorContains(t, err, "not available in this team")
	child.Steps = []model.Step{{ID: "cycle", Type: "subworkflow", DefinitionID: "parent", Version: 1}}
	_, err = ResolveDependencies(root, get)
	require.ErrorContains(t, err, "circular")
}

func TestDependencyDepthAndSizeLimits(t *testing.T) {
	get := func(id string, version *int) (model.Definition, error) {
		return dependencyDefinition(id, model.Step{ID: "next", Type: "subworkflow", DefinitionID: id + "a", Version: 1}), nil
	}
	_, err := ResolveDependencies(dependencyDefinition("root", model.Step{ID: "next", Type: "subworkflow", DefinitionID: "a", Version: 1}), get)
	require.ErrorContains(t, err, "nesting exceeds")
	child := dependencyDefinition("child", model.Step{ID: "echo", Type: "activity", Activity: "runloom.echo", With: map[string]any{"large": strings.Repeat("x", 600000)}})
	root := dependencyDefinition("root", model.Step{ID: "next", Type: "subworkflow", DefinitionID: "child", Version: 1, With: map[string]any{"large": strings.Repeat("x", 600000)}})
	_, err = ResolveDependencies(root, func(string, *int) (model.Definition, error) { return child, nil })
	require.ErrorContains(t, err, "snapshots exceed")
}

func TestSubworkflowValidation(t *testing.T) {
	valid := model.Step{ID: "call", Type: "subworkflow", DefinitionID: "child", Version: 1}
	value, err := Normalize(dependencyDefinition("parent", valid))
	require.NoError(t, err)
	require.Equal(t, 3600, *value.Steps[0].TimeoutSeconds)
	for _, mutate := range []func(*model.Step){
		func(s *model.Step) { s.Version = 0 }, func(s *model.Step) { s.DefinitionID = "" },
		func(s *model.Step) { s.Retry = &model.RetryPolicy{MaximumAttempts: 2} },
		func(s *model.Step) { s.Activity = "runloom.echo" },
	} {
		step := valid
		mutate(&step)
		_, err := Normalize(dependencyDefinition("parent", step))
		require.Error(t, err)
	}
	_, err = Normalize(dependencyDefinition("parent", model.Step{ID: "spoof", Type: "activity", Activity: "runloom.internal.record_run"}))
	require.ErrorContains(t, err, "internal")
}
