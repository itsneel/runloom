package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDraftRequestsRequireActionAndRecover(t *testing.T) {
	for _, first := range []string{"overloaded", "prose", "interrupted", "clarify", "persistent overload"} {
		t.Run(first, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var request map[string]any
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request["tool_choice"] != "required" {
					t.Error("draft action must be required")
				}
				if first == "persistent overload" || calls == 1 && first == "overloaded" {
					w.WriteHeader(503)
					return
				}
				if calls == 1 && (first == "prose" || first == "interrupted") {
					fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"I saved it"},"finish_reason":"stop"}]}`)
					if first == "prose" {
						fmt.Fprintln(w, "data: [DONE]")
					}
					return
				}
				name, args := "propose_workflow", map[string]string{"operation": "create", "workflow_json": `{"schema_version":"1.0","name":"Timer","steps":[{"id":"done","type":"end"}]}`}
				if first == "clarify" {
					name = "request_workflow_input"
					args = map[string]string{"question": "Which database connection should receive the records?"}
				}
				arguments, _ := json.Marshal(args)
				chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"id": "one", "index": 0, "type": "function", "function": map[string]any{"name": name, "arguments": string(arguments)}}}}, "finish_reason": "tool_calls"}}})
				fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n", chunk)
			}))
			defer server.Close()
			p := Provider{BaseURL: server.URL, Model: "test", Client: server.Client()}
			var messages strings.Builder
			proposals := 0
			err := p.StreamAuthoring(context.Background(), Scope{}, &AuthoringContext{AuthoringVersion: 2, SaveDraft: true}, nil, func(s string) error { messages.WriteString(s); return nil }, func(*Proposal) { proposals++ })
			if strings.Contains(messages.String(), "I saved it") {
				t.Fatal("unverified model claim reached chat")
			}
			switch first {
			case "clarify":
				if err != nil || calls != 1 || proposals != 0 || !strings.Contains(messages.String(), "Which database") {
					t.Fatalf("unexpected clarification: %v %d %d", err, calls, proposals)
				}
			case "persistent overload":
				if err == nil || !strings.Contains(err.Error(), "HTTP 503") || calls != 2 || proposals != 0 {
					t.Fatalf("unexpected outage: %v %d %d", err, calls, proposals)
				}
			default:
				if err != nil || calls != 2 || proposals != 1 {
					t.Fatalf("unexpected recovery: %v %d %d", err, calls, proposals)
				}
			}
		})
	}
}
