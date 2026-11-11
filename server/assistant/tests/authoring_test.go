package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/runloom/assistant/internal/chat"
	"github.com/google/uuid"
)

func TestAuthoringToolStream(t *testing.T) {
	for _, mode := range []string{"valid", "unavailable", "wrong-tool", "two-tools", "truncated", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil {
					t.Error("invalid provider request")
				}
				if mode == "unavailable" {
					if body["tools"] != nil {
						t.Error("general chat exposed tools")
					}
				} else if body["tools"] == nil {
					t.Error("scoped authoring missing tool")
				}
				name := "propose_child_workflow"
				if mode == "wrong-tool" {
					name = "publish_workflow"
				}
				index := 0
				if mode == "two-tools" {
					index = 1
				}
				arguments, _ := json.Marshal(map[string]string{"name": "Child", "steps_json": "[{\"id\":\"done\",\"type\":\"end\"}]", "before_step_id": ""})
				if mode == "malformed" {
					arguments = []byte(`{"bad":true}`)
				}
				for i, part := range []string{string(arguments[:len(arguments)/2]), string(arguments[len(arguments)/2:])} {
					function := map[string]any{"arguments": part}
					call := map[string]any{"index": index, "function": function}
					if i == 0 {
						function["name"] = name
						call["id"] = "call1"
						call["type"] = "function"
					}
					chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{call}}}}})
					fmt.Fprintf(w, "data: %s\n\n", chunk)
				}
				if mode != "truncated" {
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
				}
			}))
			defer upstream.Close()
			provider, _ := chat.NewProvider("openai-compatible", upstream.URL, "synthetic", "", true, 1024)
			var proposal *chat.Proposal
			context := &chat.AuthoringContext{RootID: "parent", Revision: strings.Repeat("a", 64), Workflows: json.RawMessage(`[]`)}
			if mode == "unavailable" {
				context = nil
			}
			err := provider.StreamAuthoring(contextBackground(), chat.Scope{Workflow: "parent"}, context, []chat.Message{{Role: "user", Content: "Create child"}}, func(string) error { return nil }, func(p *chat.Proposal) { proposal = p })
			if mode == "valid" {
				if err != nil || proposal == nil || proposal.Name != "Child" {
					t.Fatalf("valid proposal rejected: %v", err)
				}
			} else if err == nil || proposal != nil {
				t.Fatal("invalid stream produced usable proposal")
			}
		})
	}
}
func contextBackground() context.Context { return context.Background() }

func TestLiveReviewedAuthoring(t *testing.T) {
	for _, operation := range []string{"", "create", "update"} {
		t.Run("mode-"+operation, func(t *testing.T) { testLiveReviewedAuthoring(t, operation) })
	}
}
func testLiveReviewedAuthoring(t *testing.T, operation string) {
	workflow := "parent"
	if operation == "create" {
		workflow = ""
	}
	store := liveStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var applies atomic.Int32
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer alice" || r.Header.Get("X-Runloom-Team") != "team" {
			w.WriteHeader(403)
			return
		}
		if r.Method == "GET" {
			if operation != "" && r.Header.Get("X-Runloom-Authoring-Context") != "full" {
				t.Error("full context was not explicitly requested")
			}
			if operation == "" && r.Header.Get("X-Runloom-Authoring-Context") != "" {
				t.Error("legacy client unexpectedly requested full definitions")
			}
			fmt.Fprintf(w, `{"root_id":%q,"revision":%q,"project_id":"default","workflows":[]}`, workflow, strings.Repeat("a", 64))
			return
		}
		applies.Add(1)
		var body struct {
			RequestID string `json:"request_id"`
			Approved  bool   `json:"approved"`
			Revision  string `json:"revision"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !body.Approved || body.Revision != strings.Repeat("a", 64) {
			t.Error("apply missing bound authority")
		}
		parent, child := workflow, "child-"+body.RequestID
		if operation != "" {
			child = ""
		}
		if operation == "create" {
			parent = "workflow-" + body.RequestID
		}
		_ = json.NewEncoder(w).Encode(chat.ApplyReceipt{RequestID: body.RequestID, ParentID: parent, ChildID: child, DraftOnly: true})
	}))
	defer owner.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		args, _ := json.Marshal(map[string]string{"name": "Reviewed child", "steps_json": `[{"id":"done","type":"end"}]`, "before_step_id": ""})
		toolName := "propose_child_workflow"
		if operation != "" {
			toolName = "propose_workflow"
			args, _ = json.Marshal(map[string]string{"operation": operation, "workflow_json": `{"schema_version":"1.0","id":"parent","name":"Workflow proposal","kind":"main","steps":[{"id":"done","type":"end"}]}`})
		}
		chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call", "type": "function", "function": map[string]any{"name": toolName, "arguments": string(args)}}}}, "finish_reason": "tool_calls"}}})
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
	}))
	defer upstream.Close()
	provider, _ := chat.NewProvider("openai-compatible", upstream.URL, "synthetic", "", true, 1024)
	app := &chat.Server{Store: store, Provider: provider, Authoring: chat.NewAuthoringClient(owner.URL), CheckWorkflow: func(context.Context, *http.Request, chat.Scope) error { return nil }, Authorize: func(_ context.Context, r *http.Request, org, team string) (chat.Scope, error) {
		return chat.Scope{User: strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), Org: org, Team: team}, nil
	}}
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	done := make(chan struct{})
	go func() { app.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	id := uuid.NewString()
	base := server.URL + "/api/v1/studio/assistant/conversations"
	scope := "?org_id=org&team_id=team"
	if operation != "" {
		scope += "&authoring_version=2"
	}
	if workflow != "" {
		scope += "&workflow_id=" + workflow
	}
	request(t, server.Client(), "POST", base+scope, "alice", fmt.Sprintf(`{"id":%q}`, id), 201)
	c := request(t, server.Client(), "POST", base+"/"+id+"/messages"+scope, "alice", fmt.Sprintf(`{"request_id":%q,"content":"Create and attach a child that ends"}`, uuid.NewString()), 202)
	for i := 0; i < 100 && c.Active(); i++ {
		time.Sleep(50 * time.Millisecond)
		c = request(t, server.Client(), "GET", base+"/"+id+scope, "alice", "", 200)
	}
	job := c.Jobs[0]
	if job.Status != "completed" || job.Proposal == nil || job.ProposalHash == "" || applies.Load() != 0 {
		t.Fatal("proposal was not safely staged", job.Status, job.Error)
	}
	if operation == "create" {
		var d map[string]any
		_ = json.Unmarshal(job.Proposal.Definition, &d)
		if d["id"] != "workflow-"+job.ID {
			t.Fatal("review did not show the assigned ID")
		}
	}
	apply := base + "/" + id + "/jobs/" + job.ID + "/apply"
	request(t, server.Client(), "POST", apply+scope, "alice", `{"approved":true,"proposal_hash":"forged"}`, 409)
	body := fmt.Sprintf(`{"approved":true,"proposal_hash":%q}`, job.ProposalHash)
	request(t, server.Client(), "POST", apply+scope, "bob", body, 404)
	request(t, server.Client(), "POST", apply+"?org_id=org&team_id=team&workflow_id=another", "alice", body, 404)
	c = request(t, server.Client(), "POST", apply+scope, "alice", body, 200)
	if c.Jobs[0].Applied == nil || applies.Load() != 1 {
		t.Fatal("reviewed apply did not persist receipt")
	}
	request(t, server.Client(), "POST", apply+scope, "alice", body, 200)
	if applies.Load() != 1 {
		t.Fatal("confirmed apply replayed writes")
	}
	reloaded, err := liveStore(t).Get(ctx, id)
	if err != nil || reloaded.Jobs[0].Applied == nil {
		t.Fatal("proposal receipt did not survive reconnect")
	}
}

func TestAuthoringContextDelegatesOnlyHumanAndTeam(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer human" || r.Header.Get("X-Runloom-Team") != "team" || r.Header.Get("X-Runloom-Actor") != "" || r.Header.Get("X-Runloom-Edit-Token") != "" {
			t.Error("incorrect delegated authority")
		}
		fmt.Fprintf(w, `{"root_id":"parent","revision":%q,"project_id":"default","workflows":[]}`, strings.Repeat("a", 64))
	}))
	defer upstream.Close()
	request := httptest.NewRequest("GET", "/", nil)
	request.Header.Set("Authorization", "Bearer human")
	request.Header.Set("X-Runloom-Actor", "forged")
	request.Header.Set("X-Runloom-Edit-Token", "forged")
	result, err := chat.NewAuthoringClient(upstream.URL).Context(context.Background(), request, chat.Scope{Workflow: "parent", Team: "team"})
	if err != nil || result.RootID != "parent" {
		t.Fatal("context failed", err)
	}
}

func TestWorkflowCreateUpdateTool(t *testing.T) {
	for _, mode := range []string{"create", "update", "wrong-scope", "invalid-node", "missing-config", "unknown-field", "old-client"} {
		t.Run(mode, func(t *testing.T) {
			operation, workflow := "create", ""
			if mode == "update" {
				operation, workflow = "update", "parent"
			}
			if mode == "wrong-scope" {
				workflow = "parent"
			}
			definition := map[string]any{"schema_version": "1.0", "id": "parent", "name": "Proposed workflow", "kind": "main", "trigger": map[string]string{"type": "manual"}, "input_schema": map[string]any{"type": "object"}, "steps": []any{map[string]any{"id": "wait", "type": "wait", "seconds": 2}, map[string]any{"id": "done", "type": "end"}}}
			if mode == "invalid-node" {
				definition["steps"] = []any{map[string]any{"id": "bad", "type": "invented"}}
			}
			if mode == "missing-config" {
				definition["steps"] = []any{map[string]any{"id": "send", "type": "http"}}
			}
			if mode == "unknown-field" {
				definition["unexpected"] = true
			}
			raw, _ := json.Marshal(definition)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request map[string]any
				_ = json.NewDecoder(r.Body).Decode(&request)
				if !strings.Contains(fmt.Sprint(request["messages"]), "Authoritative Runloom workflow JSON Schema") {
					t.Error("node contract missing")
				}
				args, _ := json.Marshal(map[string]string{"operation": operation, "workflow_json": string(raw)})
				chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "proposal1", "type": "function", "function": map[string]string{"name": "propose_workflow", "arguments": string(args)}}}}}}})
				fmt.Fprintf(w, "data: %s\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", chunk)
			}))
			defer upstream.Close()
			provider, _ := chat.NewProvider("openai-compatible", upstream.URL, "synthetic", "", true, 2048)
			var proposal *chat.Proposal
			version := 2
			if mode == "old-client" {
				version = 0
			}
			err := provider.StreamAuthoring(context.Background(), chat.Scope{Workflow: workflow}, &chat.AuthoringContext{AuthoringVersion: version, RootID: workflow, Revision: strings.Repeat("a", 64), Workflows: json.RawMessage(`[]`)}, nil, func(string) error { return nil }, func(p *chat.Proposal) { proposal = p })
			if mode == "create" || mode == "update" {
				if err != nil || proposal == nil || proposal.Operation != operation || len(proposal.Definition) == 0 {
					t.Fatalf("valid workflow rejected: %v", err)
				}
			} else if err == nil || proposal != nil {
				t.Fatal("invalid proposal was reviewable")
			}
		})
	}
}

type providerRequestTransport func(*http.Request) (*http.Response, error)

func (f providerRequestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestGeminiCompatibilityOmitsOnlyUnsupportedStore(t *testing.T) {
	for _, host := range []string{"generativelanguage.googleapis.com", "api.example.test"} {
		t.Run(host, func(t *testing.T) {
			provider, err := chat.NewProvider("openai-compatible", "https://"+host+"/v1beta/openai", "synthetic-model", "", false, 1024)
			if err != nil {
				t.Fatal(err)
			}
			provider.Client = &http.Client{Transport: providerRequestTransport(func(r *http.Request) (*http.Response, error) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				value, present := body["store"]
				if host == "generativelanguage.googleapis.com" && present {
					t.Error("Gemini rejects store")
				}
				if host != "generativelanguage.googleapis.com" && (!present || value != false) {
					t.Error("other providers must retain store=false")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"Ready\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")), Header: make(http.Header)}, nil
			})}
			if err := provider.Stream(context.Background(), nil, func(string) error { return nil }); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProviderTimeoutExplainsLimit(t *testing.T) {
	provider, _ := chat.NewProvider("openai-compatible", "https://api.example.test/v1", "synthetic", "", false, 1024)
	if provider.Client.Timeout != 5*time.Minute || chat.ResponseTimeout != 5*time.Minute {
		t.Fatal("model requests must allow five minutes")
	}
	provider.Client = &http.Client{Transport: providerRequestTransport(func(*http.Request) (*http.Response, error) { return nil, os.ErrDeadlineExceeded })}
	err := provider.Stream(context.Background(), nil, func(string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "five-minute response limit") {
		t.Fatalf("timeout must explain recovery: %v", err)
	}
}

func TestProviderAvailabilityErrors(t *testing.T) {
	for _, tc := range []struct {
		code int
		want string
	}{{503, "overloaded"}, {504, "overloaded"}, {404, "model is unavailable"}, {429, "quota"}} {
		t.Run(fmt.Sprint(tc.code), func(t *testing.T) {
			p, _ := chat.NewProvider("openai-compatible", "https://api.example.test/v1", "synthetic", "", false, 1024)
			p.Client = &http.Client{Transport: providerRequestTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.code, Body: io.NopCloser(strings.NewReader("private provider error body")), Header: make(http.Header)}, nil
			})}
			err := p.Stream(context.Background(), nil, func(string) error { return nil })
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "private") {
				t.Fatalf("incorrect availability error: %v", err)
			}
		})
	}
}

func TestGeminiToolStopFinishReason(t *testing.T) {
	for _, host := range []string{"generativelanguage.googleapis.com", "api.example.test"} {
		t.Run(host, func(t *testing.T) {
			p, _ := chat.NewProvider("openai-compatible", "https://"+host+"/v1beta/openai", "synthetic", "", false, 4096)
			definition := `{"schema_version":"1.0","id":"test","name":"Test","kind":"main","trigger":{"type":"manual"},"steps":[{"id":"done","type":"end"}]}`
			args, _ := json.Marshal(map[string]string{"operation": "create", "workflow_json": definition})
			chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"id": "call1", "type": "function", "function": map[string]string{"name": "propose_workflow", "arguments": string(args)}}}}}}})
			p.Client = &http.Client{Transport: providerRequestTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("data: " + string(chunk) + "\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))}, nil
			})}
			var proposal *chat.Proposal
			err := p.StreamAuthoring(context.Background(), chat.Scope{}, &chat.AuthoringContext{AuthoringVersion: 2, Revision: strings.Repeat("a", 64), Workflows: json.RawMessage(`[]`)}, nil, func(string) error { return nil }, func(v *chat.Proposal) { proposal = v })
			if host == "generativelanguage.googleapis.com" {
				if err != nil || proposal == nil {
					t.Fatalf("Gemini tool stream rejected: %v", err)
				}
			} else if err == nil || proposal != nil {
				t.Fatal("other providers must retain strict finish validation")
			}
		})
	}
}

func TestGeminiStructuredWorkflowValidationRepair(t *testing.T) {
	for _, tc := range []struct{ autoSave, repairSucceeds bool }{{false, true}, {false, false}, {true, true}, {true, false}} {
		autoSave, repairSucceeds := tc.autoSave, tc.repairSucceeds
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			p, _ := chat.NewProvider("openai-compatible", "https://generativelanguage.googleapis.com/v1beta/openai", "synthetic", "", false, 4096)
			calls := 0
			p.Client = &http.Client{Transport: providerRequestTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil {
					t.Fatal("invalid request")
				}
				tools := body["tools"].([]any)
				function := tools[0].(map[string]any)["function"].(map[string]any)
				parameters := function["parameters"].(map[string]any)
				expectedType := "object"
				if autoSave && calls == 2 {
					expectedType = "string"
				}
				if parameters["properties"].(map[string]any)["workflow_json"].(map[string]any)["type"] != expectedType {
					t.Errorf("call %d expected workflow_json of type %s", calls, expectedType)
				}
				if calls == 2 && !strings.Contains(fmt.Sprint(body["messages"]), "Validation feedback:") {
					t.Error("repair must receive validation feedback")
				}
				end := map[string]any{"id": "done", "type": "end"}
				if calls == 1 || !repairSucceeds {
					end["invented_field"] = "unsupported node field"
					if autoSave {
						end = map[string]any{"id": "send-approval,timeout_seconds:86400,type:", "type": "approval"}
					}
				}
				definition := map[string]any{"schema_version": "1.0", "id": "test", "name": "Test", "kind": "main", "trigger": map[string]any{"type": "manual"}, "steps": []any{end}}
				var wire any = definition
				if autoSave && calls == 2 {
					if !strings.Contains(fmt.Sprint(body["messages"]), "missing property 'prompt'") {
						t.Error("missing node diagnosis")
					}
					if repairSucceeds {
						definition["steps"] = []any{map[string]any{"id": "send-approval", "type": "approval", "prompt": "Approve the request?", "timeout_seconds": 86400}}
					}
					raw, _ := json.Marshal(definition)
					wire = string(raw)
				}
				args, _ := json.Marshal(map[string]any{"operation": "create", "workflow_json": wire})
				chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"id": "call1", "type": "function", "function": map[string]string{"name": "propose_workflow", "arguments": string(args)}}}}}}})
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("data: " + string(chunk) + "\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))}, nil
			})}
			var proposal *chat.Proposal
			err := p.StreamAuthoring(context.Background(), chat.Scope{}, &chat.AuthoringContext{SaveDraft: autoSave, AuthoringVersion: 2, Revision: strings.Repeat("a", 64), Workflows: json.RawMessage(`[]`)}, nil, func(string) error { return nil }, func(v *chat.Proposal) { proposal = v })
			if calls != 2 {
				t.Fatalf("expected one repair, got %d model calls", calls)
			}
			if repairSucceeds {
				if err != nil || proposal == nil {
					t.Fatalf("valid repair rejected: %v", err)
				}
			} else if err == nil || proposal != nil {
				t.Fatal("invalid second result must not be saved")
			}
		})
	}
}
