package definition

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
)

func handlerFixture() model.Definition {
	return model.Definition{ID: "handlers", SchemaVersion: "1.0", Name: "Handlers", Steps: []model.Step{{ID: "main-end", Type: "end"}}, SignalHandlers: []model.SignalHandler{{ID: "on-change", SignalName: "customer.updated", Steps: []model.Step{{ID: "handler-end", Type: "end", With: map[string]any{"payload": "${signal.payload}"}}}}}}
}
func TestSignalHandlerContractValidation(t *testing.T) {
	original := handlerFixture()
	_, err := Normalize(original)
	require.NoError(t, err)
	for _, mode := range []string{"same-id", "same-step", "same-signal", "reserved", "empty", "remote-schema", "invalid-action"} {
		t.Run(mode, func(t *testing.T) {
			raw, _ := json.Marshal(original)
			var d model.Definition
			require.NoError(t, json.Unmarshal(raw, &d))
			switch mode {
			case "same-id":
				d.SignalHandlers[0].ID = "main-end"
			case "same-step":
				d.SignalHandlers[0].Steps[0].ID = "main-end"
			case "same-signal":
				d.Steps = []model.Step{{ID: "wait", Type: "signal", SignalName: "customer.updated", Prompt: "Wait"}}
			case "reserved":
				d.SignalHandlers[0].SignalName = "approval"
			case "empty":
				d.SignalHandlers[0].Steps = nil
			case "remote-schema":
				d.SignalHandlers[0].InputSchema = map[string]any{"$ref": "https://example.test/private"}
			case "invalid-action":
				d.SignalHandlers[0].Steps[0].Type = "invented"
			}
			_, err := Normalize(d)
			require.Error(t, err)
		})
	}
}
func TestSignalHandlerVariableHintsAreIsolated(t *testing.T) {
	d := handlerFixture()
	d.Steps = []model.Step{{ID: "main-action", Type: "activity", Activity: "runloom.echo"}}
	hints, err := VariableHints(d, "handler-end")
	require.NoError(t, err)
	found := false
	for _, h := range hints {
		if h.Path == "signal.payload" {
			found = true
		}
		require.NotContains(t, h.Path, "steps.main-action")
	}
	require.True(t, found)
}
func TestHandlerDependenciesCannotEscapeSnapshotValidation(t *testing.T) {
	d := handlerFixture()
	d.SignalHandlers[0].Steps = []model.Step{{ID: "child-call", Type: "subworkflow", DefinitionID: "missing", Version: 1}}
	called := false
	_, err := ResolveDependencies(d, func(string, *int) (model.Definition, error) {
		called = true
		return model.Definition{}, errors.New("not available")
	})
	require.Error(t, err)
	require.True(t, called)
}
