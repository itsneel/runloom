package chat

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGeneratedContractPreservesSavedCanvasLayout(t *testing.T) {
	const saved = `{"id":"root","layout":{"positions":{"@trigger":{"x":10,"y":20},"done":{"x":30,"y":40}}}}`
	context := &AuthoringContext{RootID: "root", Workflows: json.RawMessage(`[{"id":"root","definition":` + saved + `}]`)}
	for _, mode := range []string{"invalid", "omitted", "valid", "invalid-node"} {
		t.Run(mode, func(t *testing.T) {
			definition := map[string]any{"schema_version": "1.0", "id": "root", "name": "Root", "kind": "main", "steps": []any{map[string]any{"id": "done", "type": "end"}}}
			if mode == "invalid" || mode == "invalid-node" {
				definition["layout"] = map[string]any{"positions": map[string]any{"@@trigger": map[string]int{"x": 999, "y": 999}}}
			}
			if mode == "valid" {
				definition["layout"] = map[string]any{"positions": map[string]any{"done": map[string]int{"x": 55, "y": 65}}}
			}
			if mode == "invalid-node" {
				definition["steps"] = []any{map[string]any{"id": "bad", "type": "invented"}}
			}
			raw, _ := json.Marshal(definition)
			proposal := &Proposal{Operation: "update", Definition: raw}
			normalizeGeneratedLayout(proposal, context)
			if mode == "valid" {
				if string(proposal.Definition) != string(raw) {
					t.Fatal("valid generated layout must remain exact")
				}
			} else if !strings.Contains(string(proposal.Definition), `"@trigger":{"x":10,"y":20}`) {
				t.Fatal("saved layout was not preserved")
			}
			err := validateProposal(proposal)
			if mode == "invalid-node" {
				if err == nil {
					t.Fatal("canvas repair must never bypass node validation")
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNewContractInvalidCanvasDoesNotInvalidateExecution(t *testing.T) {
	p := &Proposal{Operation: "create", Definition: json.RawMessage(`{"schema_version":"1.0","id":"new","name":"New","steps":[{"id":"done","type":"end"}],"layout":{"positions":{"@@trigger":{"x":0,"y":0}}}}`)}
	normalizeGeneratedLayout(p, nil)
	if strings.Contains(string(p.Definition), `"layout"`) {
		t.Fatal("invalid optional coordinates should be left to Studio")
	}
	if err := validateProposal(p); err != nil {
		t.Fatal(err)
	}
}

func TestContractStructureChangesRearrangeWholeCanvas(t *testing.T) {
	const original = `{"schema_version":"1.0","id":"root","name":"Root","steps":[{"id":"wait","type":"wait","seconds":1},{"id":"done","type":"end"}],"layout":{"positions":{"wait":{"x":400,"y":500},"done":{"x":700,"y":500}}}}`
	context := &AuthoringContext{RootID: "root", Workflows: json.RawMessage(`[{"id":"root","definition":` + original + `}]`)}
	for _, mode := range []string{"insert", "reorder", "nested", "settings"} {
		t.Run(mode, func(t *testing.T) {
			var d map[string]any
			_ = json.Unmarshal([]byte(original), &d)
			switch mode {
			case "insert":
				d["steps"] = []any{map[string]any{"id": "wait", "type": "wait", "seconds": 1}, map[string]any{"id": "callback", "type": "callback", "callback_key": "reply", "prompt": "Continue"}, map[string]any{"id": "done", "type": "end"}}
			case "reorder":
				d["steps"] = []any{map[string]any{"id": "done", "type": "end"}, map[string]any{"id": "wait", "type": "wait", "seconds": 1}}
			case "nested":
				d["steps"] = []any{map[string]any{"id": "condition", "type": "condition", "then": []any{map[string]any{"id": "wait", "type": "wait", "seconds": 1}}}}
			case "settings":
				d["steps"].([]any)[0].(map[string]any)["seconds"] = 2
			}
			raw, _ := json.Marshal(d)
			p := &Proposal{Operation: "update", Definition: raw}
			normalizeGeneratedLayout(p, context)
			var result map[string]any
			_ = json.Unmarshal(p.Definition, &result)
			_, hasLayout := result["layout"]
			if hasLayout != (mode == "settings") {
				t.Fatalf("unexpected manual layout retention for %s", mode)
			}
		})
	}
	p := &Proposal{Operation: "create", Definition: json.RawMessage(original)}
	normalizeGeneratedLayout(p, nil)
	if strings.Contains(string(p.Definition), `"layout"`) {
		t.Fatal("new contracts must use Studio arrangement")
	}
}
