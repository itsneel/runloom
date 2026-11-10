package definition

import (
	"encoding/json"
	"fmt"

	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/prompts"
)

// Draft checks the storage envelope, not whether a workflow can execute.
// Normalize is intentionally reserved for validation, publication and execution.
func Draft(value model.Definition) (model.Definition, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return value, fmt.Errorf("draft must contain only JSON values: %w", err)
	}
	if len(raw) > MaxDefinitionBytes {
		return value, fmt.Errorf("definition exceeds the 1 MiB limit")
	}
	if !idPattern.MatchString(value.ID) {
		return value, fmt.Errorf("definition.id is invalid")
	}
	if value.SchemaVersion != "1.0" {
		return value, fmt.Errorf("schema_version must be 1.0")
	}
	if err := validateLayout(value.Layout); err != nil {
		return value, err
	}
	// Work on a copy; failed normalization must not mutate an unfinished draft.
	if err = json.Unmarshal(raw, &value); err != nil {
		return value, err
	}
	value.Version = 0
	if value.Kind == "" {
		value.Kind = "main"
	}
	if value.Trigger.Type == "" {
		value.Trigger.Type = "manual"
	}
	if value.InputSchema == nil {
		value.InputSchema = map[string]any{}
	}
	if value.Steps == nil {
		value.Steps = []model.Step{}
	}
	if len(value.SignalHandlers) > 20 {
		return value, fmt.Errorf("draft supports at most 20 signal handlers")
	}
	count := len(value.SignalHandlers)
	var walk func([]model.Step, int) error
	walk = func(steps []model.Step, depth int) error {
		if len(steps) > 0 && depth > MaxDepth {
			return fmt.Errorf("draft exceeds maximum nesting depth")
		}
		for i := range steps {
			count++
			if count > MaxSteps {
				return fmt.Errorf("draft exceeds maximum number of nodes")
			}
			s := &steps[i]
			if s.PromptTemplate != nil && s.PromptTemplate.Variables == nil {
				s.PromptTemplate.Variables = []prompts.Variable{}
			}
			if err := walk(s.Then, depth+1); err != nil {
				return err
			}
			if err := walk(s.Else, depth+1); err != nil {
				return err
			}
			for j := range s.Branches {
				if s.Branches[j].Steps == nil {
					s.Branches[j].Steps = []model.Step{}
				}
				if err := walk(s.Branches[j].Steps, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err = walk(value.Steps, 1); err != nil {
		return value, err
	}
	for i := range value.SignalHandlers {
		if value.SignalHandlers[i].Steps == nil {
			value.SignalHandlers[i].Steps = []model.Step{}
		}
		if err = walk(value.SignalHandlers[i].Steps, 1); err != nil {
			return value, err
		}
	}
	// Preserve existing defaults for complete drafts, without discarding invalid content.
	raw, _ = json.Marshal(value)
	var candidate model.Definition
	_ = json.Unmarshal(raw, &candidate)
	if normalized, err := Normalize(candidate); err == nil {
		return normalized, nil
	}
	return value, nil
}
