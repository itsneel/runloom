package definition

import (
	"encoding/json"
	"github.com/example/runloom/workflows/internal/codecontract"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestCodeSampleMatchesJSONSchema(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/spec/workflow.schema.json")
	require.NoError(t, err)
	var schemaObject map[string]any
	require.NoError(t, json.Unmarshal(raw, &schemaObject))
	schema, err := compileInputSchema(schemaObject)
	require.NoError(t, err)
	raw, err = os.ReadFile("../../testdata/examples/javascript-transform.json")
	require.NoError(t, err)
	var value map[string]any
	require.NoError(t, json.Unmarshal(raw, &value))
	require.NoError(t, schema.Validate(value))
	var definition model.Definition
	require.NoError(t, json.Unmarshal(raw, &definition))
	_, err = Normalize(definition)
	require.NoError(t, err)
	code := value["steps"].([]any)[0].(map[string]any)["code"].(map[string]any)
	code["runtime"] = "typescript"
	require.Error(t, schema.Validate(value))
}

func TestCodeDefinition(t *testing.T) {
	valid := func() model.Definition {
		return model.Definition{SchemaVersion: "1.0", ID: "javascript", Name: "JavaScript", Steps: []model.Step{{ID: "transform", Type: "code", Code: &model.JavaScriptCode{Runtime: codecontract.Runtime, Source: "return `${input.name}`;"}}}}
	}
	value, err := Normalize(valid())
	require.NoError(t, err)
	require.Equal(t, "return `${input.name}`;", value.Steps[0].Code.Source)
	require.NotNil(t, value.Steps[0].With)
	for _, mutate := range []func(*model.Step){
		func(s *model.Step) { s.Code = nil },
		func(s *model.Step) { s.Code.Runtime = "typescript" },
		func(s *model.Step) { s.Code.Source = " " },
		func(s *model.Step) { s.Activity = "runloom.echo" },
		func(s *model.Step) { s.Retry = &model.RetryPolicy{MaximumAttempts: 1} },
		func(s *model.Step) { n := 10; s.TimeoutSeconds = &n },
		func(s *model.Step) { s.Type = "activity"; s.Activity = "runloom.echo" },
		func(s *model.Step) { s.HTTP = &model.HTTPRequest{URL: "https://example.com", Method: "GET"} },
	} {
		value := valid()
		mutate(&value.Steps[0])
		_, err := Normalize(value)
		require.Error(t, err)
	}
}
