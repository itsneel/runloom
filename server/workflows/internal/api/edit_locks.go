package api

import (
	"errors"
	"log"
	"net/http"

	"github.com/example/runloom/workflows/internal/access"
	"github.com/example/runloom/workflows/internal/store"
)

const editTokenHeader = "X-Runloom-Edit-Token"

func (s *Server) editLock(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if r.Method == "GET" {
		status, err := s.modules.Projects.EditLock(r.Context(), id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, 200, status)
		return
	}
	owner := requestActor(r, "api-user")
	name := owner
	if user, ok := r.Context().Value(principalKey{}).(access.User); ok {
		name = user.Name
		if name == "" {
			name = user.Email
		}
	}
	action := map[string]string{"POST": "acquire", "PUT": "renew", "DELETE": "release"}[r.Method]
	status, err := s.modules.Projects.ChangeEditLock(r.Context(), id, owner, name, r.Header.Get(editTokenHeader), action)
	if err != nil {
		s.editLockError(w, r, id, err)
		return
	}
	if action != "renew" {
		s.audit(store.AuditEvent{DefinitionID: id, EventType: "definition.edit_lock_" + action, Actor: owner})
	}
	writeJSON(w, 200, status)
}

func (s *Server) editLockError(w http.ResponseWriter, r *http.Request, id string, err error) {
	if errors.Is(err, store.ErrEditLocked) || errors.Is(err, store.ErrEditLease) {
		status, _ := s.modules.Projects.EditLock(r.Context(), id)
		writeJSON(w, http.StatusLocked, map[string]any{"error": err.Error(), "detail": err.Error(), "lock": status, "workflow_id": id})
		return
	}
	writeStoreError(w, err)
}

func (s *Server) guardDefinitionWrite(w http.ResponseWriter, r *http.Request, id string) (func(), bool) {
	finish, err := s.modules.Projects.BeginDefinitionWrite(r.Context(), id, requestActor(r, "api-user"), r.Header.Get(editTokenHeader))
	if err != nil {
		s.editLockError(w, r, id, err)
		return nil, false
	}
	return func() {
		if err := finish(); err != nil {
			log.Print("workflow edit reservation cleanup failed; operator inspection required")
		}
	}, true
}
