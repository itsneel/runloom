package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

const editStepsTool = "edit_workflow_steps"

func workflowEditTool() map[string]any {
	text := map[string]string{"type": "string"}
	edit := map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
		"action":    map[string]any{"type": "string", "enum": []string{"update_fields", "insert_before", "delete"}},
		"target_id": text, "fields_json": text,
	}, "required": []string{"action", "target_id", "fields_json"}}
	return map[string]any{"type": "function", "function": map[string]any{
		"name": editStepsTool, "description": "Edit top-level nodes of the selected saved draft in order. update_fields changes only the supplied node fields (exclude id and type). insert_before takes a full new node and the id of the node it precedes. delete takes an existing node id and an empty object. fields_json is one serialized JSON object. All edits are validated and saved together.",
		"parameters": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"edits": map[string]any{"type": "array", "items": edit}}, "required": []string{"edits"}},
	}}
}

func selectedDefinition(ctx *AuthoringContext) json.RawMessage {
	if ctx == nil || ctx.RootID == "" {
		return nil
	}
	var workflows []struct {
		ID         string
		Definition json.RawMessage
	}
	if json.Unmarshal(ctx.Workflows, &workflows) == nil {
		for _, workflow := range workflows {
			if workflow.ID == ctx.RootID {
				return workflow.Definition
			}
		}
	}
	return nil
}

type workflowEdit struct {
	Action string `json:"action"`
	Target string `json:"target_id"`
	Fields string `json:"fields_json"`
}

type editNode map[string]json.RawMessage

func nodeIndex(steps []editNode, id string) int {
	return slices.IndexFunc(steps, func(step editNode) bool {
		var value string
		return json.Unmarshal(step["id"], &value) == nil && value == id
	})
}

func applyStepEdit(steps []editNode, edit workflowEdit) ([]editNode, error) {
	i := nodeIndex(steps, edit.Target)
	if i < 0 || edit.Target == "" {
		return nil, errors.New("target_id must identify an existing top-level node at this step")
	}
	var fields editNode
	if json.Unmarshal([]byte(edit.Fields), &fields) != nil || fields == nil {
		return nil, errors.New("fields_json must encode an object")
	}
	switch edit.Action {
	case "update_fields":
		if len(fields) == 0 || fields["id"] != nil || fields["type"] != nil {
			return nil, errors.New("supply changed node fields only, excluding id and type")
		}
		for key, value := range fields {
			steps[i][key] = value
		}
	case "insert_before":
		var id string
		if json.Unmarshal(fields["id"], &id) != nil || id == "" || nodeIndex(steps, id) >= 0 {
			return nil, errors.New("insert_before needs a new node with a distinct id")
		}
		steps = slices.Insert(steps, i, fields)
	case "delete":
		if len(fields) != 0 {
			return nil, errors.New("delete requires an empty fields object")
		}
		steps = slices.Delete(steps, i, i+1)
	default:
		return nil, errors.New("unknown node edit action")
	}
	return steps, nil
}

// Each attempt starts from a private copy of the authorized root. Only the
// complete candidate can cross the existing validation and draft-save boundary.
func buildEditedWorkflow(saved json.RawMessage, arguments string) (string, error) {
	var definition editNode
	var steps []editNode
	if json.Unmarshal(saved, &definition) != nil || json.Unmarshal(definition["steps"], &steps) != nil || len(steps) == 0 {
		return "", errors.New("selected draft steps are unavailable")
	}
	var plan struct {
		Edits []workflowEdit `json:"edits"`
	}
	d := json.NewDecoder(strings.NewReader(arguments))
	d.DisallowUnknownFields()
	if d.Decode(&plan) != nil || d.Decode(new(any)) != io.EOF || len(plan.Edits) == 0 {
		return "", errors.New("provide a nonempty ordered edits array")
	}
	for i, edit := range plan.Edits {
		var err error
		steps, err = applyStepEdit(steps, edit)
		if err != nil {
			return "", fmt.Errorf("edit %d (%s): %w", i+1, edit.Action, err)
		}
	}
	definition["steps"], _ = json.Marshal(steps)
	raw, err := json.Marshal(definition)
	return string(raw), err
}
