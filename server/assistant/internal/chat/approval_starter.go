package chat

import (
	_ "embed"
	"encoding/json"
)

const approvalStarterRequest = "Help me design an approval process with reminders and escalation."

//go:embed approval_starter.json
var approvalStarterDefinition json.RawMessage

// The built-in suggestion is standalone. Preserve conversation context for
// custom requests and selected-workflow edits. A repeat after an unsuccessful
// attempt in the same chat can use the starter as well.
func useApprovalStarter(c Conversation, context *AuthoringContext, request string) bool {
	if request != approvalStarterRequest || c.Scope.Workflow != "" || !requiresWorkflowAction(context) || context.RootID != "" {
		return false
	}
	for _, job := range c.Jobs {
		if job.Status != "failed" && job.Status != "interrupted" {
			return false
		}
	}
	for _, m := range c.Messages {
		if m.Role == "user" && m.Content != request {
			return false
		}
	}
	return true
}
