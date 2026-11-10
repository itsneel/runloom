package definition

import (
	"encoding/json"
	"fmt"

	"github.com/example/runloom/workflows/internal/model"
)

// NodeIssue identifies the actual node, including invalid/duplicate IDs via path.
type NodeIssue struct {
	NodeID  string `json:"node_id"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

// NodeIssues reuses the strict validators without stopping at the first node.
// It never normalizes the caller's draft or authorizes publication/execution.
// resolve checks team-scoped external references on a node without descendants.
func NodeIssues(value model.Definition, resolve func(model.Step) (model.Step, error)) []NodeIssue {
	issues := []NodeIssue{}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > MaxDefinitionBytes {
		return issues
	}
	var d model.Definition
	if json.Unmarshal(raw, &d) != nil {
		return issues
	}
	seen, signals := map[string]bool{}, map[string]bool{}
	count := 0
	var walk func([]model.Step, string, int)
	walk = func(steps []model.Step, path string, depth int) {
		for index, step := range steps {
			if count >= MaxSteps {
				return
			}
			stepPath := fmt.Sprintf("%s[%d]", path, index)
			add := func(err error) {
				if err != nil {
					issues = append(issues, NodeIssue{step.ID, stepPath, err.Error()})
				}
			}
			if depth > MaxDepth {
				add(fmt.Errorf("maximum node nesting depth exceeded"))
				return
			}
			// Reference resolution must not prevent diagnostics for its siblings.
			if resolve != nil {
				shallow := step
				shallow.Then, shallow.Else, shallow.Branches = nil, nil, nil
				resolved, err := resolve(shallow)
				if err != nil {
					add(err)
				} else {
					step.Prompt, step.SystemPrompt = resolved.Prompt, resolved.SystemPrompt
					step.PromptRef, step.PromptTemplate = resolved.PromptRef, resolved.PromptTemplate
				}
			}
			// Count separately: strict validation can fail before incrementing it.
			strictCount := count
			_, err := normalizeStep(step, index, len(steps), seen, signals, &strictCount, depth, path, stepPath, false)
			add(err)
			if err == nil {
				add(validatePromptMappingStep(d, step))
			}
			count++
			walk(step.Then, stepPath+".then", depth+1)
			walk(step.Else, stepPath+".else", depth+1)
			for i, branch := range step.Branches {
				prefix := stepPath
				if step.Type == "decision" {
					prefix += ".decision_actions"
				}
				if step.Type == "foreach" {
					prefix += ".foreach_body"
				}
				walk(branch.Steps, fmt.Sprintf("%s.branches[%d].steps", prefix, i), depth+1)
			}
		}
	}
	for _, h := range d.SignalHandlers {
		seen[h.ID] = true
		signals[h.SignalName] = true
		count++
	}
	walk(d.Steps, "steps", 1)
	for i, h := range d.SignalHandlers {
		walk(h.Steps, fmt.Sprintf("signal_handlers[%d].steps", i), 1)
	}
	return issues
}
