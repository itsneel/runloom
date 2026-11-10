package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io"
	"net/http"
	"strings"
	"time"
)

type Server struct {
	Store         *Store
	Provider      *Provider
	Authorize     Authorize
	CheckWorkflow WorkflowAccess
	Authoring     *AuthoringClient
}

func respond(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
func failure(w http.ResponseWriter, err error) {
	status := 503
	message := "Assistant storage unavailable. Please retry."
	var authoringFailure *authoringError
	if errors.As(err, &authoringFailure) {
		respond(w, authoringFailure.status, map[string]string{"detail": authoringFailure.detail})
		return
	}
	switch {
	case errors.Is(err, ErrWorkflowNotFound):
		status = 404
		message = err.Error()
	case errors.Is(err, ErrWorkflowUnavailable):
		message = err.Error()
	case errors.Is(err, ErrNotFound):
		status = 404
		message = err.Error()
	case errors.Is(err, ErrConflict):
		status = 409
		message = err.Error()
	case errors.Is(err, ErrUnauthorized):
		status = 401
		message = err.Error()
	case errors.Is(err, ErrForbidden):
		status = 403
		message = err.Error()
	}
	respond(w, status, map[string]string{"detail": message})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 20000))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		respond(w, 400, map[string]string{"detail": "Invalid request body."})
		return false
	}
	return true
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := s.Store.Ping(ctx); err != nil {
			failure(w, err)
			return
		}
		respond(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/api/v1/studio/assistant/", s.handle)
	return mux
}
func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	scope, err := s.Authorize(r.Context(), r, r.URL.Query().Get("org_id"), r.URL.Query().Get("team_id"))
	if err != nil {
		failure(w, err)
		return
	}
	// Scope is immutable. Never fall back to general chat on a failed workflow check.
	values := r.URL.Query()["workflow_id"]
	if len(values) > 1 || (len(values) == 1 && !workflowID.MatchString(values[0])) {
		respond(w, 400, map[string]string{"detail": "Supply one valid workflow_id or omit it for general chat."})
		return
	}
	if len(values) == 1 {
		scope.Workflow = values[0]
		if s.CheckWorkflow == nil {
			failure(w, ErrWorkflowUnavailable)
			return
		}
		if err := s.CheckWorkflow(r.Context(), r, scope); err != nil {
			failure(w, err)
			return
		}
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/studio/assistant/")
	if path == "config" && r.Method == "GET" {
		enabled := s.Authoring != nil && s.Provider != nil && (scope.Workflow != "" || r.URL.Query().Get("authoring_version") == "2")
		respond(w, 200, map[string]any{"configured": s.Provider != nil, "mode": "reviewed-authoring", "workflow_changes_enabled": enabled})
		return
	}
	if path == "conversations" {
		switch r.Method {
		case "GET":
			items, err := s.Store.List(r.Context(), &scope, r.URL.Query().Get("after"), 21)
			if err != nil {
				failure(w, err)
				return
			}
			next := ""
			if len(items) > 20 {
				items = items[:20]
				next = items[19].ID
			}
			for i := range items {
				items[i].Messages = []Message{}
				items[i].Jobs = []Job{}
			}
			respond(w, 200, map[string]any{"items": items, "next_cursor": next})
			return
		case "POST":
			var body struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			}
			if !decode(w, r, &body) {
				return
			}
			if _, err := uuid.Parse(body.ID); err != nil || len(body.Title) > 160 {
				respond(w, 400, map[string]string{"detail": "Supply a UUID and a title of at most 160 characters."})
				return
			}
			if old, err := s.Store.Get(r.Context(), body.ID); err == nil {
				if old.Scope != scope {
					failure(w, ErrNotFound)
					return
				}
				respond(w, 200, old)
				return
			}
			now := time.Now().UTC()
			c := Conversation{ID: body.ID, Scope: scope, Title: strings.TrimSpace(body.Title), Revision: 1, Messages: []Message{}, Jobs: []Job{}, CreatedAt: now, UpdatedAt: now}
			if c.Title == "" {
				c.Title = "New conversation"
			}
			if err := s.Store.Create(r.Context(), c); err != nil {
				failure(w, err)
				return
			}
			respond(w, 201, c)
			return
		}
	}
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] != "conversations" {
		http.NotFound(w, r)
		return
	}
	c, err := s.Store.Get(r.Context(), parts[1])
	if err != nil {
		failure(w, err)
		return
	}
	if c.Scope != scope {
		failure(w, ErrNotFound)
		return
	}
	if len(parts) == 2 && r.Method == "GET" {
		respond(w, 200, c)
		return
	}
	if len(parts) == 3 && parts[2] == "events" && r.Method == "GET" {
		s.stream(w, r, c)
		return
	}
	if len(parts) == 3 && parts[2] == "messages" && r.Method == "POST" {
		s.submit(w, r, c, scope)
		return
	}
	if len(parts) == 5 && parts[2] == "jobs" && parts[4] == "cancel" && r.Method == "POST" {
		for attempt := 0; attempt < 8; attempt++ {
			if attempt > 0 {
				c, err = s.Store.Get(r.Context(), parts[1])
				if err != nil {
					failure(w, err)
					return
				}
				if c.Scope != scope {
					failure(w, ErrNotFound)
					return
				}
			}
			retry := false
			for i := range c.Jobs {
				if c.Jobs[i].ID == parts[3] && c.Jobs[i].Active() {
					c.Jobs[i].Status = "cancelled"
					c.Jobs[i].UpdatedAt = time.Now().UTC()
					if err := s.Store.Save(r.Context(), &c); err != nil {
						if errors.Is(err, ErrConflict) {
							retry = true
							break
						}
						failure(w, err)
						return
					}
					respond(w, 200, c)
					return
				}
			}
			if retry {
				continue
			}
			respond(w, 409, map[string]string{"detail": "Job is no longer active."})
			return
		}
		failure(w, ErrConflict)
		return
	}
	if len(parts) == 5 && parts[2] == "jobs" && parts[4] == "apply" && r.Method == "POST" {
		s.apply(w, r, c, scope, parts[3])
		return
	}
	http.NotFound(w, r)
}

// Snapshot events are durable and idempotent: reconnect always receives the
// current revision, even after process replacement. Short streams renew auth.
func (s *Server) stream(w http.ResponseWriter, r *http.Request, c Conversation) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	revision := int64(-1)
	for {
		if c.Revision != revision {
			data, _ := json.Marshal(c)
			_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if _, err := fmt.Fprintf(w, "id: %d\nevent: snapshot\ndata: %s\n\n", c.Revision, data); err != nil {
				return
			}
			if controller.Flush() != nil {
				return
			}
			revision = c.Revision
		}
		if !c.Active() {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-deadline.C:
			return
		case <-tick.C:
		}
		var err error
		c, err = s.Store.Get(r.Context(), c.ID)
		if err != nil {
			return
		}
	}
}
