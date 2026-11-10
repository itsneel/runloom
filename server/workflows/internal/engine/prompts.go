package engine

import (
	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
)

func promptPayload(s model.Step, values *definition.Context, fields map[string]any) error {
	if s.PromptTemplate == nil {
		return nil
	}
	inputs, err := definition.Resolve(s.PromptInputs, values.Value())
	if err != nil {
		return err
	}
	fields["_runloom_prompt_template"] = s.PromptTemplate
	fields["_runloom_prompt_inputs"] = inputs
	if s.PromptRef != nil {
		fields["_runloom_prompt_ref"] = s.PromptRef
	}
	return nil
}
