// Package llmcontract validates the model-authored human-review envelope.
package llmcontract

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type Review struct {
	Required bool   `json:"hitl_required"`
	Reason   string `json:"hitl_reason"`
}

func Properties() map[string]any {
	return map[string]any{
		"hitl_required": map[string]any{"type": "boolean"},
		"hitl_reason":   map[string]any{"type": "string"},
	}
}

func Schema() map[string]any {
	p := Properties()
	p["content"] = map[string]any{"type": "string"}
	return map[string]any{"type": "object", "additionalProperties": false, "required": []string{"content", "hitl_required", "hitl_reason"}, "properties": p}
}

// Object rejects missing, unknown and duplicate fields as well as trailing data.
func Object(content string, fields ...string) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(strings.NewReader(content))
	if token, err := d.Token(); err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("LLM output must be a JSON object")
	}
	allowed := map[string]bool{}
	for _, field := range fields {
		allowed[field] = true
	}
	result := map[string]json.RawMessage{}
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok || !allowed[key] || result[key] != nil {
			return nil, fmt.Errorf("LLM output contains an unknown or duplicate field")
		}
		var raw json.RawMessage
		if d.Decode(&raw) != nil {
			return nil, fmt.Errorf("LLM output contains invalid JSON")
		}
		result[key] = raw
	}
	if token, err := d.Token(); err != nil || token != json.Delim('}') {
		return nil, fmt.Errorf("LLM output is incomplete")
	}
	var extra any
	if d.Decode(&extra) != io.EOF || len(result) != len(fields) {
		return nil, fmt.Errorf("LLM output has trailing data or missing required fields")
	}
	return result, nil
}

func ParseReview(fields map[string]json.RawMessage) (Review, error) {
	var r Review
	// json.Unmarshal accepts null for scalar types; explicitly reject it.
	if string(fields["hitl_required"]) != "true" && string(fields["hitl_required"]) != "false" {
		return r, fmt.Errorf("hitl_required must be an explicit boolean")
	}
	if err := json.Unmarshal(fields["hitl_required"], &r.Required); err != nil {
		return r, err
	}
	var reason *string
	if json.Unmarshal(fields["hitl_reason"], &reason) != nil || reason == nil {
		return r, fmt.Errorf("hitl_reason must be a string")
	}
	r.Reason = *reason
	if err := Validate(r); err != nil {
		return r, err
	}
	return r, nil
}

func Validate(r Review) error {
	if len(r.Reason) > 2000 || (r.Required && strings.TrimSpace(r.Reason) == "") || (!r.Required && r.Reason != "") {
		return fmt.Errorf("hitl_reason must explain required review (1–2000 bytes), or be empty when review is not required")
	}
	return nil
}

func Parse(content string) (string, Review, error) {
	fields, err := Object(content, "content", "hitl_required", "hitl_reason")
	if err != nil {
		return "", Review{}, err
	}
	review, err := ParseReview(fields)
	if err != nil {
		return "", review, err
	}
	var text *string
	if json.Unmarshal(fields["content"], &text) != nil || text == nil {
		return "", review, fmt.Errorf("content must be a string")
	}
	return *text, review, nil
}
