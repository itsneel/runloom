package activities

import (
	"github.com/example/runloom/workflows/internal/prompts"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"testing"
)

func TestPromptValidationBeforeProvider(t *testing.T) {
	template := prompts.Template{Text: "${vars.count}", Variables: []prompts.Variable{{Name: "count", Type: "integer", Required: true}}}
	out, err := callLLM(t.Context(), map[string]any{"_runloom_prompt_template": template, "_runloom_prompt_inputs": map[string]any{"count": "bad"}}, nil)
	require.Nil(t, out)
	var application *temporal.ApplicationError
	require.ErrorAs(t, err, &application)
	require.Equal(t, "InvalidPromptInputs", application.Type())
	require.True(t, application.NonRetryable())
}
func TestDeclaredOutputValidation(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}}, "required": []any{"answer"}}
	require.NoError(t, ValidateOutput(t.Context(), map[string]any{"schema": schema, "output": map[string]any{"answer": "yes"}}))
	require.Error(t, ValidateOutput(t.Context(), map[string]any{"schema": schema, "output": map[string]any{"answer": true}}))
}
