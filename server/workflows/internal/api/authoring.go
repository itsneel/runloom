package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"

	"github.com/example/runloom/workflows/internal/authoring"
	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/google/uuid"
)

func (s *Server) currentAuthoring(r *http.Request) (model.Definition, authoring.Context, error) {
	if r.PathValue("id") == "" {
		return model.Definition{}, authoring.Context{Integrations: definition.IntegrationCatalog(), Revision: authoring.Fingerprint("create-workflow"), ProjectID: store.DefaultProjectID, Workflows: []authoring.Outline{}}, nil
	}
	zero := 0
	root, err := s.modules.Catalog.Get(r.PathValue("id"), &zero)
	if errors.Is(err, store.ErrNotFound) {
		root, err = s.modules.Catalog.Get(r.PathValue("id"), nil)
	}
	if err != nil {
		return root, authoring.Context{}, err
	}
	projects, err := s.modules.Projects.ProjectCatalog(r.Context())
	if err != nil {
		return root, authoring.Context{}, err
	}
	graph, err := authoring.Graph(root, projects.WorkflowProject(root.ID), s.modules.Catalog.Get)
	if err == nil && r.Header.Get("X-Runloom-Authoring-Context") == "full" {
		graph.Integrations = definition.IntegrationCatalog()
		for i := range graph.Workflows {
			ref := &graph.Workflows[i]
			d, e := s.modules.Catalog.Get(ref.ID, &ref.Version)
			if e != nil {
				return root, graph, e
			}
			ref.Definition = &d
		}
		raw, _ := json.Marshal(graph)
		if len(raw) > 65536 {
			return root, graph, errors.New("workflow authoring context exceeds 64 KiB")
		}
	}
	return root, graph, err
}
func (s *Server) authoringContext(w http.ResponseWriter, r *http.Request) {
	_, graph, err := s.currentAuthoring(r)
	if err != nil {
		writeJSON(w, 409, map[string]string{"detail": "Workflow graph is unavailable or contains invalid child references. Save valid child references before requesting authoring."})
		return
	}
	writeJSON(w, 200, graph)
}
func (s *Server) applyAuthoring(w http.ResponseWriter, r *http.Request) {
	backend, ok := s.modules.Catalog.(store.Authoring)
	if !ok {
		writeJSON(w, 503, map[string]string{"detail": "Atomic authoring requires PostgreSQL or replica-set MongoDB storage."})
		return
	}
	var body struct {
		RequestID          string             `json:"request_id"`
		Restore            bool               `json:"restore,omitempty"`
		ExpectedParentHash string             `json:"expected_parent_hash,omitempty"`
		ExpectedDefinition *model.Definition  `json:"expected_definition,omitempty"`
		Revision           string             `json:"revision"`
		Approved           bool               `json:"approved"`
		Proposal           authoring.Proposal `json:"proposal"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 131072))
	d.DisallowUnknownFields()
	if d.Decode(&body) != nil || d.Decode(new(any)) != io.EOF || !body.Approved || len(body.Revision) != 64 {
		writeJSON(w, 400, map[string]string{"detail": "A reviewed proposal, revision and explicit approval are required."})
		return
	}
	requestID, err := uuid.Parse(body.RequestID)
	if err != nil {
		writeJSON(w, 400, map[string]string{"detail": "request_id must be a UUID."})
		return
	}
	body.RequestID = requestID.String()
	actor := requestActor(r, "api-user")
	rootID := r.PathValue("id")
	hashBody := body
	// Revert retries are anchored to the applied draft, not a later context read.
	if body.ExpectedParentHash != "" || body.ExpectedDefinition != nil {
		hashBody.Revision = ""
	}
	fingerprint := authoring.Fingerprint(struct {
		Actor, Root string
		Body        any
	}{actor, rootID, hashBody})
	old, err := backend.AuthoringReceipt(r.Context(), body.RequestID)
	if err == nil {
		if old.Fingerprint != fingerprint {
			writeJSON(w, 409, map[string]string{"detail": store.ErrAuthoringConflict.Error()})
			return
		}
		writeJSON(w, 200, old)
		return
	}
	if !errors.Is(err, store.ErrNotFound) {
		writeJSON(w, 503, map[string]string{"detail": "Authoring storage unavailable. Retry the same request."})
		return
	}
	_, graph, err := s.currentAuthoring(r)
	if err != nil || graph.Revision != body.Revision {
		writeJSON(w, 409, map[string]string{"detail": "Workflow or child context changed. Generate and review a fresh proposal."})
		return
	}
	childID := "child-" + body.RequestID
	if body.Proposal.Operation != "" {
		childID = ""
	}
	if body.Proposal.Operation == "create" {
		rootID = "workflow-" + body.RequestID
	}
	if (body.Proposal.Operation == "create" && r.PathValue("id") != "") || (body.Proposal.Operation != "create" && r.PathValue("id") == "") {
		writeJSON(w, 400, map[string]string{"detail": "invalid authoring scope"})
		return
	}
	// Reserve all mutable definitions in the authorized graph, in stable order.
	// Published child versions are immutable and do not require reservations.
	ids := map[string]bool{rootID: true, childID: true}
	for _, ref := range graph.Workflows {
		if ref.Version == 0 {
			ids[ref.ID] = true
		}
	}
	ordered := []string{}
	for id := range ids {
		if id == "" {
			continue
		}
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	for _, id := range ordered {
		writeRequest := r
		if id != r.PathValue("id") {
			writeRequest = r.Clone(r.Context())
			writeRequest.Header = r.Header.Clone()
			writeRequest.Header.Del(editTokenHeader)
		}
		finish, ok := s.guardDefinitionWrite(w, writeRequest, id)
		if !ok {
			return
		}
		defer finish()
	}
	root, fresh, err := s.currentAuthoring(r)
	if err != nil || fresh.Revision != body.Revision {
		writeJSON(w, 409, map[string]string{"detail": "Workflow or child context changed. Generate a fresh proposal."})
		return
	}
	parentHash := authoring.Fingerprint(root)
	if body.ExpectedDefinition != nil {
		expected, e := authoring.BuildWorkflow(root, fresh, root.ID, authoring.Proposal{Operation: "update", Definition: body.ExpectedDefinition})
		if e != nil {
			writeJSON(w, 409, map[string]string{"detail": "The saved Lumi change cannot be matched safely. Use draft history to restore a revision."})
			return
		}
		body.ExpectedParentHash = authoring.Fingerprint(expected)
	}

	if body.ExpectedParentHash != "" && body.ExpectedParentHash != parentHash {
		writeJSON(w, 409, map[string]string{"detail": "The draft changed after Lumi saved it. Your newer edits were kept; use draft history to choose a revision to restore."})
		return
	}
	sourceVersion := root.Version
	var parent, child model.Definition
	if body.Restore {
		if body.Proposal.Operation != "update" || body.Proposal.Definition == nil || body.ExpectedParentHash == "" || body.Proposal.Definition.ID != root.ID || body.Proposal.Definition.Kind != root.Kind {
			writeJSON(w, 400, map[string]string{"detail": "Restoring requires the same workflow identity and its applied draft fingerprint."})
			return
		}
		parent = *body.Proposal.Definition
		parent.Version = 0
		parent, err = definition.Draft(parent)
	} else if body.Proposal.Operation != "" {
		parent, err = authoring.BuildWorkflow(root, fresh, rootID, body.Proposal)
	} else {
		parent, child, err = authoring.Build(root, fresh, childID, body.Proposal)
	}
	if err != nil {
		writeJSON(w, 422, map[string]string{"detail": err.Error()})
		return
	}
	// Validate the complete resulting graph, not only the newly created child.
	_, err = authoring.Graph(parent, fresh.ProjectID, func(id string, v *int) (model.Definition, error) {
		if child.ID != "" && id == child.ID && v != nil && *v == 0 {
			return child, nil
		}
		if id == parent.ID && v != nil && *v == 0 {
			return parent, nil
		}
		return s.modules.Catalog.Get(id, v)
	})
	if err != nil {
		writeJSON(w, 422, map[string]string{"detail": err.Error()})
		return
	}
	result, err := backend.CommitAuthoring(r.Context(), store.AuthoringBatch{RequestID: body.RequestID, Fingerprint: fingerprint, Actor: actor, ParentHash: parentHash, ProjectID: fresh.ProjectID, SourceVersion: sourceVersion, Parent: parent, Child: child, Mode: body.Proposal.Operation, Restore: body.Restore})
	if errors.Is(err, store.ErrAuthoringConflict) {
		writeJSON(w, 409, map[string]string{"detail": err.Error()})
		return
	}
	if err != nil {
		writeJSON(w, 503, map[string]string{"detail": "Draft changes were not confirmed. Retry the same request safely."})
		return
	}
	writeJSON(w, 200, result)
}
