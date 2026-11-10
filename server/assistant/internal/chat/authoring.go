package chat

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	observability "github.com/example/runloom/sdk/go-observability"
)

//go:embed guardrails.txt
var GuardrailPrompt string

//go:embed workflow.schema.json
var WorkflowSchema string

type AuthoringContext struct {
	SaveDraft        bool            `json:"save_draft,omitempty"`
	Integrations     json.RawMessage `json:"integrations,omitempty"`
	AuthoringVersion int             `json:"authoring_version,omitempty"`
	RootID           string          `json:"root_id"`
	Revision         string          `json:"revision"`
	ProjectID        string          `json:"project_id"`
	Workflows        json.RawMessage `json:"workflows"`
}
type Proposal struct {
	Name         string            `json:"name"`
	Steps        []json.RawMessage `json:"steps"`
	BeforeStepID string            `json:"before_step_id"`
	Operation    string            `json:"operation,omitempty"`
	Definition   json.RawMessage   `json:"definition,omitempty"`
}
type ApplyReceipt struct {
	RequestID  string `json:"request_id"`
	ParentID   string `json:"parent_id"`
	ChildID    string `json:"child_id"`
	DraftOnly  bool   `json:"draft_only"`
	ParentHash string `json:"parent_hash,omitempty"`
}
type AuthoringClient struct {
	base   string
	client *http.Client
	valid  bool
}

func NewAuthoringClient(base string) *AuthoringClient {
	u, err := url.Parse(base)
	valid := err == nil && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Scheme == "http" || u.Scheme == "https")
	return &AuthoringClient{strings.TrimRight(base, "/"), &http.Client{Transport: observability.InternalTransport(nil, base), Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, valid}
}

type authoringError struct {
	status int
	detail string
}

func (e *authoringError) Error() string { return e.detail }
func (c *AuthoringClient) call(ctx context.Context, r *http.Request, scope Scope, body any, out any) error {
	if c == nil || !c.valid || (scope.Workflow != "" && !workflowID.MatchString(scope.Workflow)) {
		return ErrWorkflowUnavailable
	}
	method := "GET"
	var raw []byte
	if body != nil {
		method = "POST"
		raw, _ = json.Marshal(body)
	}
	path := "/api/v1/studio/authoring/workflows"
	if scope.Workflow != "" {
		path = "/api/v1/studio/definitions/" + url.PathEscape(scope.Workflow) + "/authoring"
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(raw))
	if err != nil {
		return ErrWorkflowUnavailable
	}
	req.Header.Set("Authorization", r.Header.Get("Authorization"))
	req.Header.Set("X-Runloom-Team", scope.Team)
	req.Header.Set("Content-Type", "application/json")
	if body != nil && scope.Workflow != "" {
		req.Header.Set("X-Runloom-Edit-Token", r.Header.Get("X-Runloom-Edit-Token"))
	}
	if body == nil && r.URL.Query().Get("authoring_version") == "2" {
		req.Header.Set("X-Runloom-Authoring-Context", "full")
	}
	res, err := c.client.Do(req)
	if err != nil {
		return ErrWorkflowUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		detail := "Workflow authoring unavailable. Retry safely."
		switch res.StatusCode {
		case 401:
			return ErrUnauthorized
		case 403:
			return ErrForbidden
		case 404:
			return ErrWorkflowNotFound
		case 409:
			detail = "The saved draft changed. Your newer edits were kept; regenerate from the latest draft or use draft history to restore a revision."
		case 423:
			var locked struct {
				ID     string `json:"workflow_id"`
				Detail string `json:"detail"`
			}
			_ = json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&locked)
			if workflowID.MatchString(locked.ID) {
				if strings.Contains(locked.Detail, "expired") {
					detail = "Your editing session for " + locked.ID + " expired. Keep any local changes and reload before retrying Lumi."
				} else {
					detail = "Workflow " + locked.ID + " is locked by another editing session. Finish editing that workflow in its other tab before retrying Lumi."
				}
			} else {
				detail = "A workflow in this draft graph is locked by another session. Finish editing it in the other tab before retrying Lumi."
			}
		case 422:
			detail = "Draft save validation failed. Check the generated node configuration and workflow references."
			var validation struct {
				Detail string `json:"detail"`
			}
			if json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&validation) == nil {
				reason := []rune(strings.TrimSpace(validation.Detail))
				if len(reason) > 512 {
					reason = append(reason[:512], '…')
				}
				if len(reason) > 0 {
					detail = "Draft save validation failed: " + string(reason)
				}
			}
		}
		return &authoringError{res.StatusCode, detail}
	}
	d := json.NewDecoder(io.LimitReader(res.Body, 65537))
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return ErrWorkflowUnavailable
	}
	return nil
}
func (c *AuthoringClient) Context(ctx context.Context, r *http.Request, scope Scope) (*AuthoringContext, error) {
	var value AuthoringContext
	err := c.call(ctx, r, scope, nil, &value)
	if err != nil {
		return nil, err
	}
	if value.RootID != scope.Workflow || len(value.Revision) != 64 || len(value.Workflows) == 0 {
		return nil, ErrWorkflowUnavailable
	}
	if r.URL.Query().Get("authoring_version") == "2" {
		value.AuthoringVersion = 2
	}
	return &value, nil
}
func proposalHash(j Job) string {
	b, _ := json.Marshal(struct {
		Context  *AuthoringContext
		Proposal *Proposal
	}{j.Context, j.Proposal})
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

func (s *Server) apply(w http.ResponseWriter, r *http.Request, c Conversation, scope Scope, id string) {
	var body struct {
		Approved bool   `json:"approved"`
		Hash     string `json:"proposal_hash"`
		Revert   bool   `json:"revert,omitempty"`
	}
	if !decode(w, r, &body) {
		return
	}
	if !body.Approved || s.Authoring == nil {
		respond(w, 400, map[string]string{"detail": "Review a workflow-scoped proposal and explicitly approve it first."})
		return
	}
	var job *Job
	for i := range c.Jobs {
		if c.Jobs[i].ID == id {
			job = &c.Jobs[i]
			break
		}
	}
	if job == nil || job.Status != "completed" || job.Proposal == nil || job.Context == nil || job.Context.RootID != scope.Workflow || body.Hash != proposalHash(*job) {
		respond(w, 409, map[string]string{"detail": "This exact proposal is not available for approval."})
		return
	}
	if body.Revert {
		s.revert(w, r, c, scope, *job)
		return
	}
	if job.Applied != nil {
		respond(w, 200, c)
		return
	}
	payload := map[string]any{"request_id": job.ID, "revision": job.Context.Revision, "approved": true, "proposal": job.Proposal}
	var receipt ApplyReceipt
	if err := s.Authoring.call(r.Context(), r, scope, payload, &receipt); err != nil {
		failure(w, err)
		return
	}
	expectedParent, expectedChild := scope.Workflow, "child-"+id
	if job.Proposal.Operation == "create" {
		expectedParent, expectedChild = "workflow-"+id, ""
	} else if job.Proposal.Operation == "update" {
		expectedChild = ""
	}
	if receipt.RequestID != id || receipt.ParentID != expectedParent || receipt.ChildID != expectedChild || !receipt.DraftOnly {
		failure(w, ErrWorkflowUnavailable)
		return
	}
	s.recordApplyReceipt(w, r, c, scope, id, receipt, false)
}

func (s *Server) recordApplyReceipt(w http.ResponseWriter, r *http.Request, c Conversation, scope Scope, id string, receipt ApplyReceipt, reverted bool) {

	for attempt := 0; attempt < 12; attempt++ {
		current, err := s.Store.Get(r.Context(), c.ID)
		if err != nil {
			failure(w, err)
			return
		}
		if current.Scope != scope {
			failure(w, ErrNotFound)
			return
		}
		found := false
		for i := range current.Jobs {
			if current.Jobs[i].ID == id {
				if reverted {
					current.Jobs[i].Reverted = &receipt
				} else {
					current.Jobs[i].Applied = &receipt
				}
				current.Jobs[i].UpdatedAt = time.Now().UTC()
				found = true
				break
			}
		}
		if !found {
			failure(w, ErrConflict)
			return
		}
		if err = s.Store.Save(r.Context(), &current); errors.Is(err, ErrConflict) {
			continue
		} else if err != nil {
			failure(w, err)
			return
		}
		respond(w, 200, current)
		return
	}
	failure(w, ErrConflict)
}

// Restores server-retained pre-edit content without a model request. The owning
// service checks the applied draft hash under its normal edit and CAS guards.
func (s *Server) revert(w http.ResponseWriter, r *http.Request, c Conversation, scope Scope, job Job) {
	if scope.Workflow == "" || job.Applied == nil || job.Proposal.Operation != "update" || job.Applied.ParentID != scope.Workflow {
		respond(w, 409, map[string]string{"detail": "Only a saved Lumi edit to this workflow can be reverted."})
		return
	}
	if job.Reverted != nil {
		respond(w, 200, c)
		return
	}
	for _, other := range c.Jobs {
		if other.Active() {
			respond(w, 409, map[string]string{"detail": "Wait for Lumi to finish before reverting a draft."})
			return
		}
	}
	var snapshots []struct {
		ID         string          `json:"id"`
		Definition json.RawMessage `json:"definition"`
	}
	if json.Unmarshal(job.Context.Workflows, &snapshots) != nil {
		failure(w, ErrConflict)
		return
	}
	var before json.RawMessage
	for _, item := range snapshots {
		if item.ID == scope.Workflow {
			before = item.Definition
			break
		}
	}
	if len(before) == 0 {
		respond(w, 409, map[string]string{"detail": "This older Lumi edit has no full draft snapshot. Restore it from workflow draft history."})
		return
	}
	fresh, err := s.Authoring.Context(r.Context(), r, scope)
	if err != nil {
		failure(w, err)
		return
	}
	requestID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("runloom-lumi-revert/"+c.ID+"/"+job.ID)).String()
	payload := map[string]any{"request_id": requestID, "revision": fresh.Revision, "approved": true, "restore": true, "proposal": Proposal{Name: job.Proposal.Name, Operation: "update", Definition: before}}
	if job.Applied.ParentHash != "" {
		payload["expected_parent_hash"] = job.Applied.ParentHash
	} else {
		payload["expected_definition"] = job.Proposal.Definition
	}
	var receipt ApplyReceipt
	if err = s.Authoring.call(r.Context(), r, scope, payload, &receipt); err != nil {
		failure(w, err)
		return
	}
	if receipt.RequestID != requestID || receipt.ParentID != scope.Workflow || receipt.ChildID != "" || !receipt.DraftOnly {
		failure(w, ErrWorkflowUnavailable)
		return
	}
	s.recordApplyReceipt(w, r, c, scope, job.ID, receipt, true)
}
