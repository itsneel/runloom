package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeneratedWorkflowPreflightRepair(t *testing.T) {
	const valid = `{"schema_version":"1.0","id":"root","name":"Root","steps":[{"id":"done","type":"end"}]}`
	for _, tc := range []struct{ name, raw string }{
		{"wrong id", strings.Replace(valid, `"id":"root"`, `"id":"wrong"`, 1)},
		{"missing name", strings.Replace(valid, `"name":"Root",`, "", 1)},
		{"invalid JSON", `{"id":`},
		{"unrepairable", strings.Replace(valid, `"id":"root"`, `"id":"wrong"`, 1)},
		{"missing steps", `{"schema_version":"1.0","id":"root","name":"Root"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				body, _ := io.ReadAll(r.Body)
				if calls == 2 && !strings.Contains(string(body), "Validation feedback:") {
					t.Error("repair request lacks feedback")
				}
				raw := tc.raw
				if calls > 1 && tc.name != "unrepairable" {
					raw = valid
				}
				args, _ := json.Marshal(map[string]string{"operation": "update", "workflow_json": raw})
				event, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call-1", "type": "function", "function": map[string]any{"name": "propose_workflow", "arguments": string(args)}}}}, "finish_reason": "tool_calls"}}})
				fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", event)
			}))
			defer server.Close()
			p := &Provider{BaseURL: server.URL, Model: "test", Client: server.Client(), MaxTokens: 1000}
			var saved *Proposal
			err := p.StreamAuthoring(context.Background(), Scope{Workflow: "root"}, &AuthoringContext{RootID: "root", AuthoringVersion: 2}, nil, func(string) error { return nil }, func(v *Proposal) { saved = v })
			if tc.name == "unrepairable" {
				if err == nil || calls != 2 || saved != nil {
					t.Fatalf("invalid draft escaped bounded repair: calls=%d saved=%v err=%v", calls, saved != nil, err)
				}
				return
			}
			if err != nil || calls != 2 || saved == nil || !strings.Contains(string(saved.Definition), `"id":"root"`) {
				t.Fatalf("repair failed: calls=%d saved=%v error=%v", calls, saved != nil, err)
			}
		})
	}
}

func TestGeneratedWorkflowUsesSchemaNodeLimit(t *testing.T) {
	steps := []any{}
	for i := 0; i < 101; i++ {
		steps = append(steps, map[string]any{"id": fmt.Sprintf("wait-%d", i), "type": "wait", "seconds": 1})
	}
	raw, _ := json.Marshal(map[string]any{"schema_version": "1.0", "id": "root", "name": "Root", "steps": steps})
	name, err := validateGeneratedWorkflow(string(raw), "update", "root")
	if err != nil {
		t.Fatal(err)
	}
	if err = validateProposal(&Proposal{Name: name, Operation: "update", Definition: raw}); err != nil {
		t.Fatal(err)
	}
}

func TestGeneratedWorkflowPreflightDoesNotAcceptInvalidID(t *testing.T) {
	name, err := validateGeneratedWorkflow(`{"id":"other","name":"Root"}`, "update", "root")
	if name != "" || err == nil || !strings.Contains(err.Error(), "selected workflow ID") {
		t.Fatalf("unsafe preflight: %q %v", name, err)
	}
}
