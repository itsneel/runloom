// Package decision owns the versioned, closed response contract for AI routing.
package decision

import (
	"encoding/json"
	"fmt"

	"github.com/example/runloom/workflows/internal/llmcontract"
)

type Choice struct {
	llmcontract.Review
	Outcome string   `json:"outcome"`
	Actions []string `json:"actions"`
	Reason  string   `json:"reason"`
}

func Schema(ids []string) map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "required": []string{"outcome", "actions", "reason", "hitl_required", "hitl_reason"}, "properties": map[string]any{
		"hitl_required": map[string]any{"type": "boolean"},
		"hitl_reason":   map[string]any{"type": "string"},
		"outcome":       map[string]any{"type": "string", "enum": []string{"actions", "finish", "handoff"}},
		"actions":       map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": ids}},
		"reason":        map[string]any{"type": "string"},
	}}
}

func Parse(content string, ids []string, mode string, remaining int) (Choice, error) {
	var c Choice
	if len(content) > 16384 {
		return c, fmt.Errorf("decision response exceeds 16 KiB")
	}
	fields, err := llmcontract.Object(content, "outcome", "actions", "reason", "hitl_required", "hitl_reason")
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal([]byte(content), &c); err != nil {
		return c, fmt.Errorf("decision must be a JSON object matching the response schema")
	}
	c.Review, err = llmcontract.ParseReview(fields)
	if err != nil {
		return c, err
	}
	return c, Validate(c, ids, mode, remaining)
}

// FromValue reads the already-parsed Activity result without reflection or JSON
// decoding in workflow code. The routing allowlist is checked again at dispatch.
func FromValue(raw any) (Choice, error) {
	var c Choice
	m, ok := raw.(map[string]any)
	if !ok || len(m) != 5 {
		return c, fmt.Errorf("decision result is not a structured object")
	}
	c.Required, ok = m["hitl_required"].(bool)
	if !ok {
		return c, fmt.Errorf("hitl_required must be an explicit boolean")
	}
	c.Review.Reason, ok = m["hitl_reason"].(string)
	if !ok {
		return c, fmt.Errorf("hitl_reason must be a string")
	}
	c.Outcome, _ = m["outcome"].(string)
	c.Reason, _ = m["reason"].(string)
	items, ok := m["actions"].([]any)
	if !ok {
		return c, fmt.Errorf("decision actions must be an array")
	}
	c.Actions = make([]string, len(items))
	for i, item := range items {
		id, ok := item.(string)
		if !ok {
			return c, fmt.Errorf("decision action ID must be text")
		}
		c.Actions[i] = id
	}
	return c, nil
}

func Validate(c Choice, ids []string, mode string, remaining int) error {
	if err := llmcontract.Validate(c.Review); err != nil {
		return err
	}
	if c.Required != (c.Outcome == "handoff") {
		return fmt.Errorf("human review requires handoff without executable actions; other outcomes require hitl_required false")
	}
	if c.Actions == nil || c.Reason == "" || len(c.Reason) > 2000 {
		return fmt.Errorf("decision requires actions and a brief reason (1–2000 bytes)")
	}
	if c.Outcome == "finish" || c.Outcome == "handoff" {
		if len(c.Actions) != 0 {
			return fmt.Errorf("finish/handoff must not select actions")
		}
		return nil
	}
	if c.Outcome != "actions" || len(c.Actions) == 0 || len(c.Actions) > remaining || (mode != "multiple" && len(c.Actions) != 1) {
		return fmt.Errorf("decision selection violates the configured mode or action limit")
	}
	seen := map[string]bool{}
	for _, id := range c.Actions {
		found := false
		for _, allowed := range ids {
			if id == allowed {
				found = true
				break
			}
		}
		if !found || seen[id] {
			return fmt.Errorf("decision selected an unavailable or duplicate action")
		}
		seen[id] = true
	}
	return nil
}
