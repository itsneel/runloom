package model

import "testing"

func TestHandlerWorkflowTypesProtectRollouts(t *testing.T) {
	old := RunInput{Definition: Definition{ID: "old"}}
	if WorkflowType("RunloomWorkflow", old) != "RunloomWorkflow" {
		t.Fatal("legacy type changed")
	}
	input := old
	input.Definitions = map[string]Definition{"child@1": {SignalHandlers: []SignalHandler{{ID: "handler"}}}}
	if WorkflowType("RunloomWorkflow", input) != "RunloomWorkflowWithSignalHandlers" {
		t.Fatal("handler dependency must opt root into the new worker type")
	}
	input = old
	input.Definition.SignalHandlers = []SignalHandler{{ID: "handler"}}
	if WorkflowType("RunloomDraftTestWorkflow", input) != "RunloomDraftTestWorkflowWithSignalHandlers" {
		t.Fatal("draft tests need the handler-capable worker type")
	}
}
