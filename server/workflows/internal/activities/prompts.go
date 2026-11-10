package activities

import (
	"context"
	"encoding/json"
	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/prompts"
	"go.temporal.io/sdk/temporal"
)

func renderPromptPayload(payload map[string]any) (map[string]any, error) {
	raw, ok := payload["_runloom_prompt_template"]
	if !ok {
		return nil, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var t prompts.Template
	if err = json.Unmarshal(encoded, &t); err != nil {
		return nil, err
	}
	inputs, _ := payload["_runloom_prompt_inputs"].(map[string]any)
	return prompts.Render(t, inputs)
}
func ValidateOutput(_ context.Context, payload map[string]any) error {
	schema, _ := payload["schema"].(map[string]any)
	if err := definition.ValidateInput(schema, payload["output"]); err != nil {
		return temporal.NewNonRetryableApplicationError("node output does not match its declared schema", "InvalidNodeOutput", err)
	}
	return nil
}
