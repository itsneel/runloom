package prompts

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRenderTypedAndSinglePass(t *testing.T) {
	template := Template{Instructions: "Use ${vars.name}", Text: "${vars.name}: ${vars.data} ${vars.count}", Variables: []Variable{{Name: "name", Type: "string", Required: true}, {Name: "data", Type: "object", Required: true}, {Name: "count", Type: "integer", Default: float64(2)}}}
	out, err := Render(template, map[string]any{"name": "${vars.count}", "data": map[string]any{"ok": true}})
	require.NoError(t, err)
	require.Equal(t, `${vars.count}: {"ok":true} 2`, out["prompt"])
	require.Equal(t, "Use ${vars.count}", out["system_prompt"])
	require.Equal(t, Hash(template), out["prompt_hash"])
	for _, values := range []map[string]any{{}, {"name": true, "data": map[string]any{}}, {"name": "a", "data": map[string]any{}, "extra": "x"}, {"name": "a", "data": map[string]any{}, "count": 1.2}} {
		_, err = Render(template, values)
		require.Error(t, err)
	}
}
func TestInvalidTemplates(t *testing.T) {
	for _, template := range []Template{
		{Text: "${input.text}"}, {Text: "${vars.missing}"}, {Text: ""},
		{Text: "Hello", Variables: []Variable{{Name: "name", Type: "string"}}},
		{Text: "Hello", Variables: []Variable{{Name: "name", Type: "unknown", Required: true}}},
		{Text: "Hello", Variables: []Variable{{Name: "name", Type: "integer", Default: "no"}}},
	} {
		require.Error(t, Validate(template))
	}
}
