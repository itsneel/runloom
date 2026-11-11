package chat

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// Database CAS is the lease/fencing mechanism across replicas. Expired running
// jobs become interrupted, never silently re-billed by an automatic retry.
func (s *Server) Run(ctx context.Context) {
	var wg sync.WaitGroup
	defer wg.Wait()
	slots := make(chan struct{}, 4)
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		items, err := s.Store.List(ctx, nil, "", 100)
		if err != nil {
			continue
		}
		for _, c := range items {
			index := c.ActiveIndex()
			if index < 0 {
				continue
			}
			j := &c.Jobs[index]
			if j.Status == "running" {
				if time.Now().After(j.Deadline) {
					j.Status = "interrupted"
					j.Error = "Response interrupted by service restart or timeout. Partial response saved; send a new message to continue."
					j.UpdatedAt = time.Now().UTC()
					_ = s.Store.Save(ctx, &c)
				}
				continue
			}
			select {
			case slots <- struct{}{}:
			default:
				continue
			}
			j.Status = "running"
			// Queued turns may outlive a provider configuration restart.
			if s.Provider != nil && j.Kind != "approval_starter" {
				j.Model = s.Provider.Model
			} else {
				j.Model = ""
			}
			j.UpdatedAt = time.Now().UTC()
			j.Deadline = time.Now().Add(ResponseTimeout + 10*time.Second)
			if s.Store.Save(ctx, &c) != nil {
				<-slots
				continue
			}
			wg.Add(1)
			go func(c Conversation) { defer wg.Done(); defer func() { <-slots }(); s.work(ctx, c) }(c)
		}
	}
}
func (s *Server) work(parent context.Context, c Conversation) {
	ctx, cancel := context.WithTimeout(parent, ResponseTimeout)
	defer cancel()
	index := c.ActiveIndex()
	jobID := c.Jobs[index].ID
	conversationID := c.ID
	// Cancellation is observed even while the provider is silent.
	watchDone := make(chan struct{})
	defer close(watchDone)
	go func() {
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-watchDone:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				latest, err := s.Store.Get(ctx, conversationID)
				running := false
				for _, j := range latest.Jobs {
					if j.ID == jobID && j.Status == "running" {
						running = true
					}
				}
				if err != nil || !running {
					cancel()
					return
				}
			}
		}
	}()
	messages := []Message{}
	// Queue order is execution order. Future queued turns must not leak into
	// the current prompt, particularly when stop-and-send takes priority.
	for i, j := range c.Jobs {
		if i > index {
			break
		}
		for _, m := range c.Messages {
			if m.JobID == j.ID && (m.Role == "user" || (j.Status == "completed" && (j.Model != "" || j.Kind == "approval_starter"))) {
				messages = append(messages, m)
			}
		}
	}
	messageIndex := -1
	for i, m := range c.Messages {
		if m.JobID == jobID && m.Role == "assistant" {
			messageIndex = i
		}
	}
	if messageIndex < 0 {
		return
	}
	content := ""
	last := time.Now()
	var err error
	if s.Provider == nil && c.Jobs[index].Kind != "approval_starter" {
		content = UnconfiguredReply
		c.Jobs[index].Model = ""
		c.Jobs[index].Status = "completed"
	} else {
		attachProposal := func(proposal *Proposal) {
			if proposal.Operation == "create" {
				var value map[string]any
				if json.Unmarshal(proposal.Definition, &value) == nil {
					value["id"] = "workflow-" + jobID
					value["kind"] = "main"
					value["version"] = 0
					value["schema_version"] = "1.0"
					proposal.Definition, _ = json.Marshal(value)
				}
			}
			c.Jobs[index].Proposal = proposal
			c.Jobs[index].ProposalHash = proposalHash(c.Jobs[index])
		}
		if c.Jobs[index].Kind == "approval_starter" {
			name, check := validateGeneratedWorkflow(string(approvalStarterDefinition), "create", c.Scope.Workflow)
			proposal := &Proposal{Name: name, Operation: "create", Definition: approvalStarterDefinition}
			err = check
			if err == nil {
				err = validateProposal(proposal)
			}
			if err == nil {
				attachProposal(proposal)
				content = "Built the approval starter. It requests a review, sends one reminder after 24 hours without a decision, and escalates after another 24 hours. A human approval or rejection finishes the workflow. The request and both notification addresses are inputs. Studio will confirm the draft save."
			}
		} else {
			err = s.Provider.StreamAuthoring(ctx, c.Scope, c.Jobs[index].Context, messages, func(delta string) error {
				content += delta
				if time.Since(last) < 100*time.Millisecond {
					return nil
				}
				last = time.Now()
				c.Messages[messageIndex].Content = content
				c.Jobs[index].UpdatedAt = last.UTC()
				if s.saveProgress(ctx, c, index, messageIndex) != nil {
					return ErrConflict
				}
				return nil
			}, attachProposal)
		}
		c.Jobs[index].Status = "completed"
		if err == nil && requiresWorkflowAction(c.Jobs[index].Context) && c.Jobs[index].Proposal == nil {
			c.Jobs[index].Kind = "needs_input"
		}
		if err != nil {
			c.Jobs[index].Proposal = nil
			c.Jobs[index].ProposalHash = ""
			c.Jobs[index].Status = "failed"
			c.Jobs[index].Error = "Response interrupted. Partial response saved; send a new message to retry."
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				c.Jobs[index].Error = responseTimeoutMessage
			} else if ctx.Err() == nil && err != ErrConflict {
				c.Jobs[index].Error = err.Error()
			}
		}
	}
	c.Messages[messageIndex].Content = content
	c.Jobs[index].UpdatedAt = time.Now().UTC()
	// A separate short context permits shutdown to persist the terminal state.
	finish, done := context.WithTimeout(context.Background(), 3*time.Second)
	defer done()
	_ = s.saveProgress(finish, c, index, messageIndex)
}

// Merge only this running job's progress into the latest aggregate. Enqueueing
// must not interrupt generation, and cancelled/expired jobs must never revive.
func (s *Server) saveProgress(ctx context.Context, progress Conversation, jobIndex, messageIndex int) error {
	job := progress.Jobs[jobIndex]
	for attempt := 0; attempt < 12; attempt++ {
		latest, err := s.Store.Get(ctx, progress.ID)
		if err != nil {
			return err
		}
		found := false
		for i, j := range latest.Jobs {
			if j.ID != job.ID {
				continue
			}
			if j.Status != "running" {
				return ErrConflict
			}
			latest.Jobs[i] = job
			found = true
			break
		}
		if !found {
			return ErrConflict
		}
		for i, m := range latest.Messages {
			if m.ID == progress.Messages[messageIndex].ID {
				latest.Messages[i] = progress.Messages[messageIndex]
				break
			}
		}
		if err = s.Store.Save(ctx, &latest); err == ErrConflict {
			continue
		}
		return err
	}
	return ErrConflict
}
