package chat

import "testing"

func TestApprovalStarterRequiresStandaloneCreateIntent(t *testing.T) {
	context := &AuthoringContext{AuthoringVersion: 2, SaveDraft: true}
	retry := Conversation{Jobs: []Job{{Status: "failed"}}, Messages: []Message{{Role: "user", Content: approvalStarterRequest}}}
	for _, c := range []Conversation{{}, retry} {
		if !useApprovalStarter(c, context, approvalStarterRequest) {
			t.Fatal("expected approval starter")
		}
	}
	for _, c := range []Conversation{{Scope: Scope{Workflow: "selected"}}, {Jobs: []Job{{Status: "completed"}}}, {Messages: []Message{{Role: "user", Content: "Use a two-hour deadline"}}}} {
		if useApprovalStarter(c, context, approvalStarterRequest) {
			t.Fatal("custom authoring context was ignored")
		}
	}
	if useApprovalStarter(Conversation{}, context, approvalStarterRequest+" Use a two-hour deadline.") || useApprovalStarter(Conversation{}, nil, approvalStarterRequest) {
		t.Fatal("unrelated request selected starter")
	}
	name, err := validateGeneratedWorkflow(string(approvalStarterDefinition), "create", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateProposal(&Proposal{Name: name, Operation: "create", Definition: approvalStarterDefinition}); err != nil {
		t.Fatal(err)
	}
}
