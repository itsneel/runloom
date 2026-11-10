package api

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
)

func TestScheduleSampleContracts(t *testing.T) {
	read := func(path string, target any) {
		t.Helper()
		raw, err := os.ReadFile("../../testdata/" + path)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, target))
	}
	var schemaValue any
	read("spec/schedule.schema.json", &schemaValue)
	compiler := jsonschema.NewCompiler()
	require.NoError(t, compiler.AddResource("urn:runloom:schedule-sample", schemaValue))
	schema, err := compiler.Compile("urn:runloom:schedule-sample")
	require.NoError(t, err)
	b, err := store.New(t.TempDir())
	require.NoError(t, err)
	for _, id := range []string{"reusable-greeting", "parent-with-subworkflow", "parent-with-two-subworkflows", "parallel-greeting", "parent-with-parallel-subworkflows"} {
		var value model.Definition
		read("examples/"+id+".json", &value)
		_, err = b.SaveDraft(value)
		require.NoError(t, err)
		_, err = b.Publish(id)
		require.NoError(t, err)
	}
	for _, name := range []string{"hourly-greeting", "weekday-greetings"} {
		t.Run(name, func(t *testing.T) {
			var document any
			read("examples/schedules/"+name+".json", &document)
			require.NoError(t, schema.Validate(document))
			var body scheduleRequest
			read("examples/schedules/"+name+".json", &body)
			require.True(t, body.Paused, "samples must not activate recurring execution by default")
			_, err := scheduleSpec(&body, time.Now())
			require.NoError(t, err)
			root, err := b.Get(body.DefinitionID, &body.Version)
			require.NoError(t, err)
			require.NoError(t, definition.ValidateInput(root.InputSchema, body.Input))
			s := &Server{modules: store.Bind(b)}
			input, err := s.prepareRun(httptest.NewRequest("POST", "/api/v1/studio/schedules", nil), root, body.Input)
			require.NoError(t, err)
			require.Contains(t, input.Definitions, "reusable-greeting@1")
		})
	}
}
