package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

type evalCase struct {
	Name    string
	Prompt  string
	Outcome string
	Expect  map[string]any
	Tool    string
	Args    json.RawMessage
}

// Match declared fields, with exact order and length for execution paths.
func checkEvalFields(t *testing.T, path string, actual, expected any) {
	t.Helper()
	switch want := expected.(type) {
	case map[string]any:
		got, ok := actual.(map[string]any)
		if !ok {
			t.Fatalf("%s: expected object", path)
		}
		for key, value := range want {
			checkEvalFields(t, path+"."+key, got[key], value)
		}
	case []any:
		got, ok := actual.([]any)
		if !ok || len(got) != len(want) {
			t.Fatalf("%s: expected %d ordered elements, got %d", path, len(want), len(got))
		}
		for i, value := range want {
			checkEvalFields(t, fmt.Sprintf("%s[%d]", path, i), got[i], value)
		}
	default:
		if !reflect.DeepEqual(actual, expected) {
			t.Errorf("%s: got %v, want %v", path, actual, expected)
		}
	}
}

// The scripted and live providers feed the same production tool path and grader.
// Grading uses the resulting definition; the model may choose any supported tool.
func TestLumiWorkflowEval(t *testing.T) {
	live := os.Getenv("RUNLOOM_LUMI_EVAL_LIVE") == "1"
	var provider *Provider
	if live {
		var err error
		provider, err = ConfiguredProvider(os.Getenv)
		if err != nil || provider == nil {
			t.Fatalf("live eval requires assistant provider configuration: %v", err)
		}
		t.Logf("live model %s", provider.Model)
	}
	raw, err := os.ReadFile("testdata/workflow-eval.json")
	if err != nil {
		t.Fatal(err)
	}
	var suite struct {
		Draft json.RawMessage
		Cases []evalCase
	}
	if err := json.Unmarshal(raw, &suite); err != nil {
		t.Fatal(err)
	}
	if len(suite.Cases) == 0 {
		t.Fatal("empty eval set")
	}
	var draft map[string]any
	if err := json.Unmarshal(suite.Draft, &draft); err != nil {
		t.Fatal(err)
	}
	root, ok := draft["id"].(string)
	if !ok || root == "" {
		t.Fatal("eval draft needs an id")
	}
	for _, tc := range suite.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			p := provider
			if !live {
				p = replayTool(t, tc.Tool, tc.Args)
			}
			authoring := &AuthoringContext{AuthoringVersion: 2, SaveDraft: true}
			if tc.Outcome == "update" {
				authoring.RootID = root
				authoring.Workflows, err = json.Marshal([]any{map[string]any{"id": root, "definition": suite.Draft}})
				if err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), ResponseTimeout)
			defer cancel()
			var result *Proposal
			var response strings.Builder
			err := p.StreamAuthoring(ctx, Scope{Workflow: authoring.RootID}, authoring, []Message{{Role: "user", Content: tc.Prompt}}, func(s string) error { response.WriteString(s); return nil }, func(v *Proposal) { result = v })
			if err != nil {
				t.Fatalf("generation/validation: %v", err)
			}
			if tc.Outcome == "question" {
				term, _ := tc.Expect["question_contains"].(string)
				if term == "" || result != nil || !strings.Contains(strings.ToLower(response.String()), strings.ToLower(term)) {
					t.Fatal("expected clarification about the missing connection and no draft")
				}
				return
			}
			if result == nil || result.Operation != tc.Outcome {
				t.Fatalf("expected a %s contract", tc.Outcome)
			}
			var actual map[string]any
			if err := json.Unmarshal(result.Definition, &actual); err != nil {
				t.Fatal(err)
			}
			checkEvalFields(t, "contract", actual, tc.Expect)
			if tc.Outcome == "update" {
				for k, v := range draft {
					if _, changed := tc.Expect[k]; !changed && !reflect.DeepEqual(actual[k], v) {
						t.Errorf("preservation: draft field %s changed", k)
					}
				}
			}
			t.Log(strings.TrimSpace(response.String()))
		})
	}
}

func replayTool(t *testing.T, name string, args any) *Provider {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Tools []struct{ Function struct{ Name string } }
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		offered := false
		for _, tool := range request.Tools {
			if tool.Function.Name == name {
				offered = true
			}
		}
		if !offered {
			t.Errorf("tool %s was not offered", name)
			w.WriteHeader(400)
			return
		}
		writeEvalTool(w, name, args)
	}))
	t.Cleanup(server.Close)
	return &Provider{BaseURL: server.URL, Model: "scripted", Client: server.Client(), MaxTokens: 4096}
}

func writeEvalTool(w http.ResponseWriter, name string, args any) {
	raw, _ := json.Marshal(args)
	event, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "eval", "type": "function", "function": map[string]string{"name": name, "arguments": string(raw)}}}}, "finish_reason": "tool_calls"}}})
	fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", event)
}
