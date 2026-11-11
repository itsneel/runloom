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

func TestWorkflowEditSequenceIsAtomic(t *testing.T) {
	const saved = `{"id":"root","name":"Test","schema_version":"1.0","steps":[{"id":"done","type":"end"}]}`
	insert := workflowEdit{"insert_before", "done", `{"id":"new","type":"wait","seconds":1}`}
	for _, tc := range []struct {
		name  string
		edit  workflowEdit
		valid bool
	}{
		{"update inserted node", workflowEdit{"update_fields", "new", `{"seconds":3}`}, true},
		{"missing target", workflowEdit{"delete", "missing", "{}"}, false},
		{"duplicate id", insert, false},
		{"identity change", workflowEdit{"update_fields", "new", `{"id":"changed"}`}, false},
		{"broken payload", workflowEdit{"update_fields", "new", "null"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args, _ := json.Marshal(map[string]any{"edits": []workflowEdit{insert, tc.edit}})
			got, err := buildEditedWorkflow(json.RawMessage(saved), string(args))
			if tc.valid {
				if err != nil || !strings.Contains(got, `"seconds":3`) {
					t.Fatalf("later edit did not see inserted node: %v", err)
				}
			} else if got != "" || err == nil || !strings.Contains(err.Error(), "edit 2") {
				t.Fatalf("bad second edit produced a candidate or lost its diagnostic: %v", err)
			}
		})
	}
}

func TestInvalidEditNeverProducesDraft(t *testing.T) {
	args := map[string]any{"edits": []workflowEdit{{"update_fields", "delay", `{"seconds":0}`}}}
	p := replayTool(t, editStepsTool, args)
	a := &AuthoringContext{AuthoringVersion: 2, SaveDraft: true, RootID: "root", Workflows: json.RawMessage(`[{"id":"root","definition":{"id":"root","name":"Timer","schema_version":"1.0","steps":[{"id":"delay","type":"wait","seconds":2}]}}]`)}
	var progress strings.Builder
	err := p.StreamAuthoring(context.Background(), Scope{Workflow: "root"}, a, nil, func(s string) error { progress.WriteString(s); return nil }, func(*Proposal) { t.Error("invalid change escaped validation") })
	if err == nil || strings.Count(progress.String(), "Correcting") != 1 {
		t.Fatalf("expected one correction before failure: %v", err)
	}
}

func TestCreateFillsOnlyAnOmittedSchemaVersion(t *testing.T) {
	const raw = `{"id":"root","kind":"main","name":"Timer","steps":[{"id":"delay","type":"wait","seconds":3}]}`
	candidate, err := prepareGeneratedWorkflow(raw, "create", &AuthoringContext{})
	if err != nil || !strings.Contains(string(candidate.Definition), `"schema_version":"1.0"`) {
		t.Fatalf("create must supply the draft format: %v", err)
	}
	if _, err := prepareGeneratedWorkflow(strings.Replace(raw, `"kind"`, `"schema_version":"2.0","kind"`, 1), "create", &AuthoringContext{}); err == nil {
		t.Fatal("unsupported version accepted")
	}
	if _, err := prepareGeneratedWorkflow(raw, "update", &AuthoringContext{RootID: "root"}); err == nil {
		t.Fatal("incomplete replacement accepted")
	}
}

func TestOutageDuringCorrectionRetriesTheCorrection(t *testing.T) {
	var calls int
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		seconds := 0
		if calls == 3 {
			body, err := io.ReadAll(r.Body)
			if err != nil || !strings.Contains(string(body), "Validation feedback:") {
				t.Error("outage discarded the correction stage")
			}
			seconds = 2
		}
		raw := fmt.Sprintf(`{"schema_version":"1.0","name":"Timer","steps":[{"id":"delay","type":"wait","seconds":%d}]}`, seconds)
		writeEvalTool(w, "propose_workflow", map[string]string{"operation": "create", "workflow_json": raw})
	}))
	defer s.Close()
	p := &Provider{BaseURL: s.URL, Client: s.Client(), Model: "scripted"}
	var result *Proposal
	err := p.StreamAuthoring(context.Background(), Scope{}, &AuthoringContext{AuthoringVersion: 2, SaveDraft: true}, nil, func(string) error { return nil }, func(candidate *Proposal) { result = candidate })
	if err != nil || calls != 3 || result == nil {
		t.Fatalf("correction retry: calls=%d, err=%v", calls, err)
	}
}
