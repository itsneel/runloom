package chat

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// The entire submission, including interruption and queue order, is atomic.
// Repeating a request ID never cancels a newer job or creates another response.
func (s *Server) submit(w http.ResponseWriter, r *http.Request, c Conversation, scope Scope) {
	var body struct {
		SaveDraft bool   `json:"save_draft"`
		ID        string `json:"request_id"`
		Content   string `json:"content"`
		Mode      string `json:"mode"`
	}
	if !decode(w, r, &body) {
		return
	}
	body.Content = strings.TrimSpace(body.Content)
	if body.Mode == "" {
		body.Mode = "send"
	}
	if _, err := uuid.Parse(body.ID); err != nil || body.Content == "" || len(body.Content) > 8000 {
		respond(w, 400, map[string]string{"detail": "Message must contain 1–8000 bytes and a UUID request_id."})
		return
	}
	if body.Mode != "send" && body.Mode != "queue" && body.Mode != "stop_and_send" {
		respond(w, 400, map[string]string{"detail": "Mode must be send, queue or stop_and_send."})
		return
	}
	for attempt := 0; attempt < 12; attempt++ {
		if attempt > 0 {
			var err error
			c, err = s.Store.Get(r.Context(), c.ID)
			if err != nil {
				failure(w, err)
				return
			}
			if c.Scope != scope {
				failure(w, ErrNotFound)
				return
			}
		}
		for _, job := range c.Jobs {
			if job.RequestID != body.ID {
				continue
			}
			mode := job.Mode
			if mode == "" {
				mode = "send"
			}
			if mode != body.Mode || job.SaveDraft != body.SaveDraft {
				failure(w, ErrConflict)
				return
			}
			for _, m := range c.Messages {
				if m.JobID == job.ID && m.Role == "user" && m.Content != body.Content {
					failure(w, ErrConflict)
					return
				}
			}
			respond(w, 200, c)
			return
		}
		active := c.ActiveIndex()
		if active >= 0 && body.Mode == "send" {
			respond(w, 409, map[string]string{"detail": "A response is active. Queue this message or stop and send."})
			return
		}
		total := len(body.Content)
		queued := 0
		for _, m := range c.Messages {
			total += len(m.Content)
		}
		for _, j := range c.Jobs {
			if j.Status == "queued" {
				queued++
			}
		}
		if len(c.Jobs) >= 40 {
			respond(w, 409, map[string]string{"code": "conversation_limit", "detail": "Conversation limit reached: 40 submissions per chat. Move unsent messages to a new chat to continue."})
			return
		}
		if total > 120000 {
			respond(w, 409, map[string]string{"code": "conversation_size_limit", "detail": "Conversation size limit reached: 120,000 bytes. Move unsent messages to a new chat to continue."})
			return
		}
		if queued >= 10 && body.Mode != "stop_and_send" {
			respond(w, 409, map[string]string{"code": "queue_limit", "detail": "Queue is full: 10 responses are waiting. Wait for a response to finish, then retry delivery."})
			return
		}
		now := time.Now().UTC()
		job := Job{SaveDraft: body.SaveDraft, ID: uuid.NewString(), RequestID: body.ID, Mode: body.Mode, Kind: "planning", Status: "queued", CreatedAt: now, UpdatedAt: now}
		reply := ""
		if s.Provider == nil {
			job.Status = "completed"
			reply = UnconfiguredReply
			if body.SaveDraft {
				job.Status = "failed"
				job.Error = "Workflow generation requires a configured model provider. Ask an administrator to configure Lumi."
				reply = ""
			}
		} else {
			job.Model = s.Provider.Model
			if s.Authoring != nil && (scope.Workflow != "" || r.URL.Query().Get("authoring_version") == "2") {
				context, err := s.Authoring.Context(r.Context(), r, scope)
				if err != nil {
					failure(w, err)
					return
				}
				context.SaveDraft = body.SaveDraft
				job.Context = context
				job.Kind = "authoring"
			} else if body.SaveDraft {
				job.Status = "failed"
				job.Error = "Workflow authoring context is unavailable. Reload Studio and check the Assistant service configuration."
			}
		}
		if active >= 0 && body.Mode == "stop_and_send" {
			c.Jobs[active].Status = "cancelled"
			c.Jobs[active].UpdatedAt = now
			// Preserve older queued turns, but give the interrupting turn priority.
			c.Jobs = append(c.Jobs, Job{})
			copy(c.Jobs[active+2:], c.Jobs[active+1:])
			c.Jobs[active+1] = job
		} else {
			c.Jobs = append(c.Jobs, job)
		}
		c.Messages = append(c.Messages, Message{uuid.NewString(), "user", body.Content, job.ID, now}, Message{uuid.NewString(), "assistant", reply, job.ID, now})
		if len(c.Jobs) == 1 {
			title := []rune(body.Content)
			if len(title) > 70 {
				title = title[:70]
			}
			c.Title = string(title)
		}
		if err := s.Store.Save(r.Context(), &c); err != nil {
			if errors.Is(err, ErrConflict) {
				continue
			}
			failure(w, err)
			return
		}
		respond(w, 202, c)
		return
	}
	failure(w, ErrConflict)
}
