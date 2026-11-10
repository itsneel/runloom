package definition

import (
	"fmt"
	"sort"
	"strings"

	"github.com/example/runloom/workflows/internal/model"
)

type VariableHint struct {
	Path        string `json:"path"`
	Type        string `json:"type"`
	Optional    bool   `json:"optional"`
	Description string `json:"description"`
	Dynamic     bool   `json:"dynamic,omitempty"`
}

func VariableHints(d model.Definition, target string) ([]VariableHint, error) {
	count := 0
	var bounded func([]model.Step, int) bool
	bounded = func(steps []model.Step, depth int) bool {
		if depth > 20 {
			return false
		}
		for _, s := range steps {
			count++
			if count > 500 {
				return false
			}
			if !bounded(s.Then, depth+1) || !bounded(s.Else, depth+1) {
				return false
			}
			for _, b := range s.Branches {
				if !bounded(b.Steps, depth+1) {
					return false
				}
			}
		}
		return true
	}
	if !bounded(model.ExecutionSteps(d), 0) {
		return nil, fmt.Errorf("variable analysis supports at most 500 nodes and 20 nesting levels")
	}
	base := []VariableHint{}
	flattenSchema("input", d.InputSchema, false, &base, 0)
	var result []VariableHint
	found := false
	var walk func([]model.Step, []VariableHint, bool) []VariableHint
	walk = func(steps []model.Step, visible []VariableHint, optional bool) []VariableHint {
		visible = append([]VariableHint{}, visible...)
		for _, s := range steps {
			if s.ID == target {
				result = append([]VariableHint{}, visible...)
				found = true
				return visible
			}
			if s.Type == "condition" {
				before := len(visible)
				a := walk(s.Then, visible, true)
				b := walk(s.Else, visible, true)
				visible = append(visible, a[before:]...)
				visible = append(visible, b[before:]...)
			}
			if s.Type == "parallel" {
				prior := append([]VariableHint{}, visible...)
				for _, b := range s.Branches {
					after := walk(b.Steps, prior, optional)
					visible = append(visible, after[len(prior):]...)
				}
			}
			if s.Type == "decision" {
				local := append([]VariableHint{}, visible...)
				local = append(local, VariableHint{Path: "decision.round", Type: "integer", Description: "Current action round"}, VariableHint{Path: "decision.reason", Type: "string", Description: "Reason for the action"}, VariableHint{Path: "decision.previous", Type: "array", Description: "Prior decision rounds"})
				for _, b := range s.Branches {
					walk(b.Steps, local, optional)
				}
			}
			if s.Type == "foreach" {
				local := append([]VariableHint{}, visible...)
				local = append(local, VariableHint{Path: "loop.item", Type: "unknown", Dynamic: true, Description: "Current For Each item"}, VariableHint{Path: "loop.index", Type: "integer", Description: "Zero-based iteration index"})
				for _, b := range s.Branches {
					walk(b.Steps, local, optional)
				}
			}
			flattenSchema("steps."+s.ID+".output", stepSchema(s), optional, &visible, 0)
		}
		return visible
	}
	walk(d.Steps, base, false)
	for _, handler := range d.SignalHandlers {
		local := append([]VariableHint(nil), base...)
		local = append(local, VariableHint{Path: "signal.payload", Type: "unknown", Dynamic: true, Description: "Payload for this handler invocation"}, VariableHint{Path: "signal.actor", Type: "string", Description: "Verified sender or temporal-client"}, VariableHint{Path: "signal.name", Type: "string", Description: "Received event name"}, VariableHint{Path: "signal.event_id", Type: "string", Optional: true, Description: "Optional event idempotency key"})
		flattenSchema("signal.payload", handler.InputSchema, false, &local, 0)
		walk(handler.Steps, local, false)
	}
	if !found {
		return nil, fmt.Errorf("node not found")
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}
func flattenSchema(path string, schema map[string]any, optional bool, out *[]VariableHint, depth int) {
	if depth > 12 || len(*out) > 2000 {
		return
	}
	kind, _ := schema["type"].(string)
	if kind == "" {
		kind = "unknown"
	}
	description, _ := schema["description"].(string)
	properties, _ := schema["properties"].(map[string]any)
	dynamic := kind == "unknown" || (kind == "object" && len(properties) == 0 && schema["additionalProperties"] != false)
	*out = append(*out, VariableHint{Path: path, Type: kind, Optional: optional, Description: description, Dynamic: dynamic})
	required := map[string]bool{}
	if list, ok := schema["required"].([]any); ok {
		for _, v := range list {
			if name, ok := v.(string); ok {
				required[name] = true
			}
		}
	}
	if list, ok := schema["required"].([]string); ok {
		for _, v := range list {
			required[v] = true
		}
	}
	keys := []string{}
	for k := range properties {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if strings.Contains(k, ".") || !referencePattern.MatchString("${"+k+"}") {
			continue
		}
		if child, ok := properties[k].(map[string]any); ok {
			flattenSchema(path+"."+k, child, optional || !required[k], out, depth+1)
		}
	}
}
func stepSchema(s model.Step) map[string]any {
	if s.OutputSchema != nil {
		return s.OutputSchema
	}
	fields := map[string]string{}
	switch s.Type {
	case "transform":
		return map[string]any{"type": "object"}
	case "foreach", "merge":
		return map[string]any{"type": "array"}
	case "collection":
		if s.Data != nil && s.Data.Operation == "count" {
			return map[string]any{"type": "object", "properties": map[string]any{"count": map[string]any{"type": "integer"}}, "required": []string{"count"}}
		}
		if s.Data != nil && s.Data.Operation == "sum" {
			return map[string]any{"type": "object", "properties": map[string]any{"sum": map[string]any{"type": "number"}, "count": map[string]any{"type": "integer"}}, "required": []string{"sum", "count"}}
		}
		return map[string]any{"type": "array"}
	case "llm":
		if s.LLMTask != nil {
			schema := s.LLMTask.Schema()
			properties := schema["properties"].(map[string]any)
			properties["model"], properties["usage"] = map[string]any{"type": "string"}, map[string]any{"type": "object"}
			return schema
		}
		fields = map[string]string{"content": "string", "hitl_required": "boolean", "hitl_reason": "string", "model": "string", "usage": "object"}
	case "decision":
		fields = map[string]string{"hitl_required": "boolean", "hitl_reason": "string", "outcome": "string", "reason": "string", "rounds": "array", "results": "object", "selected_actions": "array", "actions_executed": "integer"}
	case "approval":
		fields = map[string]string{"approved": "boolean", "comment": "string", "actor": "string"}
	case "callback", "signal", "pause":
		fields = map[string]string{"payload": "unknown", "actor": "string"}
	case "wait":
		fields = map[string]string{"waited_seconds": "integer"}
	case "http", "integration":
		fields = map[string]string{"status_code": "integer", "body": "unknown"}
	case "database":
		fields = map[string]string{"rows": "array", "affected": "integer", "truncated": "boolean"}
	case "activity":
		switch s.Activity {
		case "runloom.sum":
			fields = map[string]string{"value": "number"}
		case "runloom.compose":
			fields = map[string]string{"message": "string"}
		default:
			return map[string]any{}
		}
	default:
		return map[string]any{}
	}
	properties := map[string]any{}
	required := []string{}
	for name, kind := range fields {
		properties[name] = map[string]any{"type": kind}
		required = append(required, name)
	}
	return map[string]any{"type": "object", "properties": properties, "required": required}
}

func ValidatePromptMappings(d model.Definition) error {
	var walk func([]model.Step) error
	walk = func(steps []model.Step) error {
		for _, s := range steps {
			if err := validatePromptMappingStep(d, s); err != nil {
				return err
			}
			if err := walk(s.Then); err != nil {
				return err
			}
			if err := walk(s.Else); err != nil {
				return err
			}
			for _, b := range s.Branches {
				if err := walk(b.Steps); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(model.ExecutionSteps(d))
}

func validatePromptMappingStep(d model.Definition, s model.Step) error {
	if s.PromptTemplate != nil {
		hints, err := VariableHints(d, s.ID)
		if err != nil {
			return err
		}
		types := map[string]string{}
		dynamicPaths := map[string]bool{}
		for _, h := range hints {
			types[h.Path] = h.Type
			dynamicPaths[h.Path] = h.Dynamic
		}
		for _, v := range s.PromptTemplate.Variables {
			_, topString := s.PromptInputs[v.Name].(string)
			for _, value := range promptMappingStrings(s.PromptInputs[v.Name]) {
				for _, m := range referencePattern.FindAllStringSubmatch(value, -1) {
					kind, known := types[m[1]]
					if !known {
						dynamic := false
						for path := range types {
							if dynamicPaths[path] && strings.HasPrefix(m[1], path+".") {
								dynamic = true
								break
							}
						}
						if !dynamic {
							return fmt.Errorf("%s: unavailable prompt input reference %s", s.ID, m[1])
						}
					}
					if topString && m[0] == value && known && kind != "unknown" && kind != v.Type && !(kind == "integer" && v.Type == "number") {
						return fmt.Errorf("%s: %s expects %s, reference is %s", s.ID, v.Name, v.Type, kind)
					}
					if topString && m[0] != value && v.Type != "string" {
						return fmt.Errorf("%s: interpolated prompt input %s must be a string", s.ID, v.Name)
					}
				}
			}
		}
	}
	return nil
}

func promptMappingStrings(value any) []string {
	switch v := value.(type) {
	case string:
		return []string{v}
	case map[string]any:
		result := []string{}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			result = append(result, promptMappingStrings(v[k])...)
		}
		return result
	case []any:
		result := []string{}
		for _, item := range v {
			result = append(result, promptMappingStrings(item)...)
		}
		return result
	default:
		return nil
	}
}
