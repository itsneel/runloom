package definition

import (
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/prompts"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestExecutionAwareVariableHints(t *testing.T) {
	d := model.Definition{InputSchema: map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}, "required": []any{"name"}}, Steps: []model.Step{
		{ID: "first", Type: "llm"},
		{ID: "fork", Type: "parallel", Branches: []model.Branch{{ID: "a", Steps: []model.Step{{ID: "a-first", Type: "wait"}, {ID: "inside-a", Type: "llm"}}}, {ID: "b", Steps: []model.Step{{ID: "b-first", Type: "wait"}}}}},
		{ID: "choice", Type: "condition", Then: []model.Step{{ID: "optional", Type: "wait"}}},
		{ID: "route", Type: "decision", Branches: []model.Branch{{ID: "action", Steps: []model.Step{{ID: "local", Type: "llm"}}}}},
		{ID: "last", Type: "llm"},
	}}
	hints, err := VariableHints(d, "inside-a")
	require.NoError(t, err)
	paths := map[string]VariableHint{}
	for _, h := range hints {
		paths[h.Path] = h
	}
	require.Equal(t, "string", paths["input.name"].Type)
	require.False(t, paths["input.name"].Optional)
	require.Contains(t, paths, "steps.first.output.hitl_required")
	require.Contains(t, paths, "steps.a-first.output.waited_seconds")
	require.NotContains(t, paths, "steps.b-first.output")
	require.NotContains(t, paths, "steps.last.output")
	hints, err = VariableHints(d, "last")
	require.NoError(t, err)
	paths = map[string]VariableHint{}
	for _, h := range hints {
		paths[h.Path] = h
	}
	require.Contains(t, paths, "steps.b-first.output")
	require.True(t, paths["steps.optional.output"].Optional)
	require.NotContains(t, paths, "steps.local.output")
	hints, err = VariableHints(d, "local")
	require.NoError(t, err)
	require.Contains(t, hints, VariableHint{Path: "decision.round", Type: "integer", Description: "Current action round"})
}
func TestPromptMappingTypesAndPinnedSnapshot(t *testing.T) {
	template := prompts.Template{Text: "${vars.text}", Variables: []prompts.Variable{{Name: "text", Type: "string", Required: true}}}
	d := model.Definition{SchemaVersion: "1.0", ID: "example", Name: "Example", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}, "count": map[string]any{"type": "integer"}}}, Steps: []model.Step{{ID: "ask", Type: "llm", Model: "default", PromptTemplate: &template, PromptInputs: map[string]any{"text": "${input.name}"}}}}
	_, err := Normalize(d)
	require.NoError(t, err)
	for _, ref := range []string{"${input.count}", "${input.typo}", "${steps.later.output}"} {
		d.Steps[0].PromptInputs["text"] = ref
		_, err = Normalize(d)
		require.Error(t, err)
	}
	d.Steps[0].PromptInputs["text"] = "${input.name}"
	d.Steps[0].PromptRef = &prompts.Reference{ID: "shared", Version: 1}
	published := prompts.Entry{ID: "shared", Version: 1, Template: template, Hash: prompts.Hash(template)}
	d, err = ResolvePrompts(d, func(id string, version int) (prompts.Entry, error) {
		require.Equal(t, "shared", id)
		require.Equal(t, 1, version)
		return published, nil
	})
	require.NoError(t, err)
	require.Equal(t, published.Hash, d.Steps[0].PromptRef.Hash)
	_, err = Normalize(d)
	require.NoError(t, err)
}
