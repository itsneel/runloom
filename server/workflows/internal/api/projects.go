package api

import (
	"errors"
	"fmt"
	"net/http"
	"sort"

	"github.com/example/runloom/workflows/internal/store"
)

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	catalog, err := s.modules.Projects.ProjectCatalog(r.Context())
	if err != nil {
		writeProjectError(w, err)
		return
	}
	projects := make([]store.Project, 0, len(catalog.Projects))
	for _, p := range catalog.Projects {
		projects = append(projects, p)
	}
	sort.Slice(projects, func(i, j int) bool {
		if (projects[i].ID == store.DefaultProjectID) != (projects[j].ID == store.DefaultProjectID) {
			return projects[i].ID == store.DefaultProjectID
		}
		return projects[i].Name < projects[j].Name
	})
	writeJSON(w, http.StatusOK, projects)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	p, err := s.modules.Projects.CreateProject(r.Context(), body.Name)
	if err != nil {
		writeProjectError(w, err)
		return
	}
	s.audit(store.AuditEvent{EventType: "project.created", Actor: requestActor(r, "api-user"), Payload: p})
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) renameProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	p, err := s.modules.Projects.RenameProject(r.Context(), r.PathValue("project"), body.Name)
	if err != nil {
		writeProjectError(w, err)
		return
	}
	s.audit(store.AuditEvent{EventType: "project.renamed", Actor: requestActor(r, "api-user"), Payload: p})
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) moveDefinitionProject(w http.ResponseWriter, r *http.Request) {
	finish, ok := s.guardDefinitionWrite(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	defer finish()
	var body struct {
		ProjectID string `json:"project_id"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	id := r.PathValue("id")
	catalog, err := s.modules.Projects.ProjectCatalog(r.Context())
	if err != nil {
		writeProjectError(w, err)
		return
	}
	if err := s.modules.Projects.AssignProject(r.Context(), id, body.ProjectID); err != nil {
		writeProjectError(w, err)
		return
	}
	s.audit(store.AuditEvent{DefinitionID: id, EventType: "definition.project_moved", Actor: requestActor(r, "api-user"), Payload: map[string]string{"from_project_id": catalog.WorkflowProject(id), "project_id": body.ProjectID}})
	writeJSON(w, http.StatusOK, map[string]string{"definition_id": id, "project_id": body.ProjectID})
}

func writeProjectError(w http.ResponseWriter, err error) {
	code := http.StatusServiceUnavailable
	switch {
	case errors.Is(err, store.ErrProjectName):
		code = http.StatusBadRequest
	case errors.Is(err, store.ErrProjectConflict), errors.Is(err, store.ErrProjectNotEmpty), errors.Is(err, store.ErrDefaultProject):
		code = http.StatusConflict
	case errors.Is(err, store.ErrProjectNotFound), errors.Is(err, store.ErrNotFound):
		code = http.StatusNotFound
	default:
		err = fmt.Errorf("project storage unavailable; retry")
	}
	writeError(w, code, err)
}
