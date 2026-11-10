package definition

import (
	"encoding/json"
	"fmt"

	"github.com/example/runloom/workflows/internal/model"
)

const MaxSubworkflowDepth = 8
const MaxSubworkflowDefinitions = 100

func DefinitionKey(id string, version int) string { return fmt.Sprintf("%s@%d", id, version) }

// ResolveDependencies runs outside workflow code. Every pinned dependency is
// embedded in the root run input so replay never fetches a mutable definition.
func ResolveDependencies(root model.Definition, get func(string, *int) (model.Definition, error)) (map[string]model.Definition, error) {
	resolved := map[string]model.Definition{}
	visiting := map[string]bool{}
	checkedDepth := map[string]int{}
	var walkDefinition func(model.Definition, int) error
	var walkSteps func([]model.Step, int) error
	walkSteps = func(steps []model.Step, depth int) error {
		for _, step := range steps {
			if step.Type == "subworkflow" {
				key := DefinitionKey(step.DefinitionID, step.Version)
				if visiting[key] {
					return fmt.Errorf("circular subworkflow reference: %s", key)
				}
				child, found := resolved[key]
				if !found {
					if len(resolved) >= MaxSubworkflowDefinitions {
						return fmt.Errorf("too many subworkflow definitions (maximum %d)", MaxSubworkflowDefinitions)
					}
					var err error
					child, err = get(step.DefinitionID, &step.Version)
					if err != nil {
						return fmt.Errorf("subworkflow %s is not available in this team: %w", key, err)
					}
					if child.Kind != "child" {
						return fmt.Errorf("subworkflow %s must be published with kind child", key)
					}
					resolved[key] = child
				}
				if err := walkDefinition(child, depth+1); err != nil {
					return err
				}
			}
			if err := walkSteps(step.Then, depth); err != nil {
				return err
			}
			if err := walkSteps(step.Else, depth); err != nil {
				return err
			}
			for _, branch := range step.Branches {
				if err := walkSteps(branch.Steps, depth); err != nil {
					return err
				}
			}
		}
		return nil
	}
	walkDefinition = func(value model.Definition, depth int) error {
		if depth > MaxSubworkflowDepth {
			return fmt.Errorf("subworkflow nesting exceeds %d levels", MaxSubworkflowDepth)
		}
		key := DefinitionKey(value.ID, value.Version)
		if checked, ok := checkedDepth[key]; ok && checked >= depth {
			return nil
		}
		if _, err := Normalize(value); err != nil {
			return err
		}
		visiting[key] = true
		defer delete(visiting, key)
		if err := walkSteps(model.ExecutionSteps(value), depth); err != nil {
			return err
		}
		checkedDepth[key] = depth
		return nil
	}
	if err := walkDefinition(root, 0); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(struct {
		Root        model.Definition
		Definitions map[string]model.Definition
	}{root, resolved})
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxDefinitionBytes {
		return nil, fmt.Errorf("workflow and subworkflow snapshots exceed 1 MiB")
	}
	return resolved, nil
}
