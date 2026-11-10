package api

import (
	"errors"
	"net/http"

	"github.com/example/runloom/workflows/internal/store"
)

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("project")
	if err := s.modules.Projects.DeleteProject(r.Context(), id); err != nil {
		writeProjectError(w, err)
		return
	}
	s.audit(store.AuditEvent{EventType: "project.deleted", Actor: requestActor(r, "api-user"), Payload: map[string]string{"project_id": id}})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteDraftWorkflow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	finish, ok := s.guardDefinitionWrite(w, r, id)
	if !ok {
		return
	}
	defer finish()
	if err := s.modules.Catalog.DeleteDraft(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrPublishedWorkflow) {
			writeError(w, http.StatusConflict, err)
		} else {
			writeStoreError(w, err)
		}
		return
	}
	s.audit(store.AuditEvent{DefinitionID: id, EventType: "definition.deleted", Actor: requestActor(r, "api-user"), Payload: map[string]any{"draft_only": true}})
	w.WriteHeader(http.StatusNoContent)
}
