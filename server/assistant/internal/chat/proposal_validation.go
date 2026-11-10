package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

var schemaOnce sync.Once
var proposalSchema *jsonschema.Schema
var proposalLayoutSchema *jsonschema.Schema
var proposalSchemaError error
var nodeSchemas = map[string]*jsonschema.Schema{}

// Validate against the bundled contract before persisting a reviewable proposal.
// Execution semantics, graph references, credentials and concurrency are checked
// separately by the owner service on Apply, with the human's current credentials.
func initializeProposalSchemas() {
	schemaOnce.Do(func() {
		var document any
		if proposalSchemaError = json.Unmarshal([]byte(WorkflowSchema), &document); proposalSchemaError != nil {
			return
		}
		c := jsonschema.NewCompiler()
		const location = "https://runloom.local/schemas/workflow-1.0.json"
		if proposalSchemaError = c.AddResource(location, document); proposalSchemaError == nil {
			proposalSchema, proposalSchemaError = c.Compile(location)
			if proposalSchemaError == nil {
				proposalLayoutSchema, proposalSchemaError = c.Compile(location + "#/properties/layout")
				defs := document.(map[string]any)["$defs"].(map[string]any)
				for _, alternative := range defs["step"].(map[string]any)["oneOf"].([]any) {
					ref := alternative.(map[string]any)["$ref"].(string)
					properties := defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)["properties"].(map[string]any)
					types := properties["type"].(map[string]any)
					schema, err := c.Compile(location + ref)
					if err != nil {
						proposalSchemaError = err
						return
					}
					if name, ok := types["const"].(string); ok {
						nodeSchemas[name] = schema
					}
					if names, ok := types["enum"].([]any); ok {
						for _, name := range names {
							nodeSchemas[name.(string)] = schema
						}
					}
				}
			}
		}
	})
}

func validateProposal(p *Proposal) error {
	initializeProposalSchemas()
	if proposalSchemaError != nil {
		return errors.New("workflow contract is unavailable")
	}
	var d map[string]any
	if p.Operation == "" {
		raw, _ := json.Marshal(p.Steps)
		var steps any
		_ = json.Unmarshal(raw, &steps)
		d = map[string]any{"schema_version": "1.0", "id": "child-proposal", "name": p.Name, "kind": "child", "trigger": map[string]any{"type": "manual"}, "steps": steps}
	} else if json.Unmarshal(p.Definition, &d) != nil || d == nil {
		return errors.New("invalid workflow JSON")
	}
	if p.Operation == "create" {
		d["id"] = "workflow-proposal"
		d["kind"] = "main"
	}
	// Draft references use version 0; the published contract requires positive pins.
	// This validation-only copy never changes the exact reviewed draft proposal.
	var pin func(any)
	pin = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			if v["type"] == "subworkflow" && v["version"] == float64(0) {
				v["version"] = 1
			}
			for _, child := range v {
				pin(child)
			}
		case []any:
			for _, child := range v {
				pin(child)
			}
		}
	}
	pin(d["steps"])
	pin(d["signal_handlers"])
	if err := proposalSchema.Validate(d); err != nil {
		if detail := nodeDiagnostic(d["steps"], "steps"); detail != nil {
			return &proposalValidationError{cause: detail, message: "Workflow validation: " + detail.Error()}
		}
		handlers, _ := d["signal_handlers"].([]any)
		for i, value := range handlers {
			handler, _ := value.(map[string]any)
			if detail := nodeDiagnostic(handler["steps"], fmt.Sprintf("signal_handlers[%d].steps", i)); detail != nil {
				return &proposalValidationError{cause: detail, message: "Workflow validation: " + detail.Error()}
			}
		}
		return &proposalValidationError{cause: err, message: "Workflow validation: " + shortDiagnostic(err)}
	}
	return nil
}

// Report the selected node contract instead of every alternative in step.oneOf.
func nodeDiagnostic(value any, path string) error {
	steps, _ := value.([]any)
	for index, step := range steps {
		node, _ := step.(map[string]any)
		kind, _ := node["type"].(string)
		here := fmt.Sprintf("%s[%d]", path, index)
		schema := nodeSchemas[kind]
		if schema == nil {
			return fmt.Errorf("%s has an unsupported or missing node type %q", here, kind)
		}
		if err := schema.Validate(node); err != nil {
			for _, key := range []string{"then", "else"} {
				if problem := nodeDiagnostic(node[key], here+"."+key); problem != nil {
					return problem
				}
			}
			branches, _ := node["branches"].([]any)
			for i, entry := range branches {
				branch, _ := entry.(map[string]any)
				if problem := nodeDiagnostic(branch["steps"], fmt.Sprintf("%s.branches[%d].steps", here, i)); problem != nil {
					return problem
				}
			}
			return fmt.Errorf("%s (%s): %s", here, kind, shortDiagnostic(err))
		}
	}
	return nil
}

func shortDiagnostic(err error) string {
	_, detail, found := strings.Cut(err.Error(), "\n")
	if !found {
		return "invalid configuration"
	}
	message := []rune(strings.TrimSpace(detail))
	if len(message) > 500 {
		return string(message[:500]) + "…"
	}
	return string(message)
}

// Gemini accepts structured tool inputs, but rejects recursive schemas whose
// arrays require at least one element. Relax only this generation hint; the
// authoritative schema above still validates every returned definition unchanged.
func geminiWorkflowParameters() map[string]any {
	var definition map[string]any
	_ = json.Unmarshal([]byte(WorkflowSchema), &definition)
	definitions := definition["$defs"]
	delete(definition, "$defs")
	delete(definition, "$id")
	delete(definition, "$schema")
	var relax func(any)
	relax = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			if v["type"] == "array" {
				if _, ok := v["minItems"]; ok {
					v["minItems"] = 0
				}
			}
			for _, child := range v {
				relax(child)
			}
		case []any:
			for _, child := range v {
				relax(child)
			}
		}
	}
	parameters := map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"operation": map[string]any{"type": "string", "enum": []string{"create", "update"}}, "workflow_json": definition}, "required": []string{"operation", "workflow_json"}, "$defs": definitions}
	relax(parameters)
	return parameters
}

// Gemini's forced function mode needs a shallow schema. Constrain node kinds
// here; the complete recursive schema validates the response before saving.
func geminiDraftParameters() map[string]any {
	initializeProposalSchemas()
	names := make([]string, 0, len(nodeSchemas))
	for name := range nodeSchemas {
		names = append(names, name)
	}
	sort.Strings(names)
	var contract map[string]any
	_ = json.Unmarshal([]byte(WorkflowSchema), &contract)
	defs := contract["$defs"].(map[string]any)
	fields := map[string]any{}
	for _, alt := range defs["step"].(map[string]any)["oneOf"].([]any) {
		ref := alt.(map[string]any)["$ref"].(string)
		for name, raw := range defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)["properties"].(map[string]any) {
			property := raw.(map[string]any)
			if link, ok := property["$ref"].(string); ok {
				property = defs[strings.TrimPrefix(link, "#/$defs/")].(map[string]any)
			}
			hint := map[string]any{}
			if typ, ok := property["type"].(string); ok {
				hint["type"] = typ
				if typ == "array" {
					hint["items"] = map[string]any{}
				}
			}
			fields[name] = hint
		}
	}
	fields["id"] = defs["identifier"]
	fields["type"] = map[string]any{"type": "string", "enum": names}
	steps := map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": fields, "required": []string{"id", "type"}, "additionalProperties": false}}
	properties := contract["properties"].(map[string]any)
	properties["schema_version"] = map[string]any{"type": "string", "enum": []string{"1.0"}}
	properties["kind"] = map[string]any{"type": "string", "enum": []string{"main", "child"}}
	properties["steps"] = steps
	properties["id"] = defs["identifier"]
	properties["trigger"] = map[string]any{"type": "object", "properties": map[string]any{"type": map[string]any{"type": "string", "enum": []string{"manual", "webhook"}}, "path": map[string]string{"type": "string"}, "method": map[string]string{"type": "string"}}, "required": []string{"type"}}
	properties["signal_handlers"] = map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"id": defs["identifier"], "name": map[string]string{"type": "string"}, "signal_name": map[string]string{"type": "string"}, "input_schema": map[string]string{"type": "object"}, "steps": steps}, "required": []string{"id", "signal_name", "steps"}}}
	properties["layout"] = map[string]string{"type": "object"}
	return map[string]any{"type": "object", "properties": map[string]any{"operation": map[string]any{"type": "string", "enum": []string{"create", "update"}}, "workflow_json": map[string]any{"type": "object", "properties": properties, "required": []string{"schema_version", "id", "name", "kind", "trigger", "steps"}}}, "required": []string{"operation", "workflow_json"}}
}

type proposalValidationError struct {
	cause   error
	message string
}

func (e *proposalValidationError) Error() string {
	if e.message != "" {
		return e.message
	}
	return "Generated workflow contract is invalid. Lumi could not correct the node fields or input mappings; the draft was not changed."
}

// Canvas coordinates do not affect workflow execution. If generated layout is
// invalid, preserve the authorized saved layout for updates; new nodes can be
// placed by Studio. Never repair or drop execution fields here.
func normalizeGeneratedLayout(p *Proposal, authoring *AuthoringContext) {
	if p.Operation == "" {
		return
	}
	initializeProposalSchemas()
	if proposalSchemaError != nil {
		return
	}
	var definition map[string]json.RawMessage
	if json.Unmarshal(p.Definition, &definition) != nil {
		return
	}
	// Execution structure changes invalidate coordinates, including positions
	// that were valid before a node was inserted, removed or moved to a branch.
	// Studio's deterministic graph layout owns the new arrangement.
	rearrange := p.Operation == "create"
	if p.Operation == "update" && authoring != nil {
		var workflows []struct {
			ID         string          `json:"id"`
			Definition json.RawMessage `json:"definition"`
		}
		if json.Unmarshal(authoring.Workflows, &workflows) == nil {
			for _, workflow := range workflows {
				if workflow.ID == authoring.RootID {
					var saved map[string]json.RawMessage
					if json.Unmarshal(workflow.Definition, &saved) == nil && len(saved["steps"]) > 0 {
						rearrange = workflowTopology(saved["steps"]) != workflowTopology(definition["steps"]) || handlerTopology(saved["signal_handlers"]) != handlerTopology(definition["signal_handlers"])
					}
					break
				}
			}
		}
	}
	if rearrange {
		delete(definition, "layout")
		if normalized, err := json.Marshal(definition); err == nil {
			p.Definition = normalized
		}
		return
	}
	raw, exists := definition["layout"]
	if !exists && p.Operation != "update" {
		return
	}
	var layout any
	if exists && json.Unmarshal(raw, &layout) == nil && proposalLayoutSchema.Validate(layout) == nil {
		return
	}
	delete(definition, "layout")
	if p.Operation == "update" && authoring != nil {
		var workflows []struct {
			ID         string          `json:"id"`
			Definition json.RawMessage `json:"definition"`
		}
		if json.Unmarshal(authoring.Workflows, &workflows) == nil {
			for _, workflow := range workflows {
				if workflow.ID != authoring.RootID {
					continue
				}
				var saved map[string]json.RawMessage
				if json.Unmarshal(workflow.Definition, &saved) == nil {
					if original, ok := saved["layout"]; ok {
						var valid any
						if json.Unmarshal(original, &valid) == nil && proposalLayoutSchema.Validate(valid) == nil {
							definition["layout"] = original
						}
					}
				}
				break
			}
		}
	}
	if normalized, err := json.Marshal(definition); err == nil && len(normalized) <= 48000 {
		p.Definition = normalized
	}
}

// Only graph identity, order and nesting influence arrangement; changing a
// prompt, timeout, mapping or label must not discard a manually arranged canvas.
func workflowTopology(raw json.RawMessage) string {
	var steps []map[string]json.RawMessage
	if len(raw) == 0 || string(raw) == "null" {
		return "[]"
	}
	if json.Unmarshal(raw, &steps) != nil {
		return string(raw)
	}
	result := make([]map[string]any, 0, len(steps))
	for _, step := range steps {
		node := map[string]any{"id": string(step["id"]), "type": string(step["type"])}
		for _, key := range []string{"then", "else"} {
			node[key] = workflowTopology(step[key])
		}
		var branches []struct {
			ID    string          `json:"id"`
			Steps json.RawMessage `json:"steps"`
		}
		_ = json.Unmarshal(step["branches"], &branches)
		paths := make([]any, 0, len(branches))
		for _, branch := range branches {
			paths = append(paths, []string{branch.ID, workflowTopology(branch.Steps)})
		}
		node["branches"] = paths
		result = append(result, node)
	}
	encoded, _ := json.Marshal(result)
	return string(encoded)
}

func handlerTopology(raw json.RawMessage) string {
	var handlers []struct {
		ID    string          `json:"id"`
		Steps json.RawMessage `json:"steps"`
	}
	if json.Unmarshal(raw, &handlers) != nil {
		return "[]"
	}
	result := make([]any, 0, len(handlers))
	for _, handler := range handlers {
		result = append(result, []string{handler.ID, workflowTopology(handler.Steps)})
	}
	encoded, _ := json.Marshal(result)
	return string(encoded)
}

// Keep authoring preflight failures in the same bounded correction path as schema
// failures. The schema owns node counts; a second smaller limit rejects valid edits.
func validateGeneratedWorkflow(raw, operation, selectedID string) (string, error) {
	fail := func(message string) (string, error) {
		return "", &proposalValidationError{cause: errors.New(message), message: message + " The draft was not changed."}
	}
	if len(raw) > 48000 {
		return fail("Generated workflow contract exceeds the response size limit.")
	}
	var d struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if json.Unmarshal([]byte(raw), &d) != nil {
		return fail("Generated workflow contract must be a complete JSON object.")
	}
	if strings.TrimSpace(d.Name) == "" {
		return fail("Generated workflow contract is missing its workflow name.")
	}
	if operation == "update" && d.ID != selectedID {
		return fail("Generated workflow contract must preserve the selected workflow ID.")
	}
	return d.Name, nil
}
