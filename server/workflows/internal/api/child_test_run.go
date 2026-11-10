package api

import (
	"fmt"
	"net/http"

	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/google/uuid"
	"go.temporal.io/sdk/client"
)

// The gateway requires a verified editor/admin in the selected team. This is
// deliberately separate from production start, webhook and schedule endpoints.
func (s *Server) testWorkflow(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Definition *model.Definition `json:"definition"`
		Version    int               `json:"version"`
		Input      map[string]any    `json:"input"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, 422, err)
		return
	}
	if body.Definition == nil && body.Version < 1 {
		writeError(w, 422, fmt.Errorf("provide a draft definition snapshot, or a published child version >= 1"))
		return
	}
	version := body.Version
	if body.Definition != nil {
		if body.Version != 0 || body.Definition.ID != r.PathValue("id") {
			writeError(w, 422, fmt.Errorf("supply a matching draft definition without a published version"))
			return
		}
		version = 0
	}
	// Require an existing team-owned draft so project budgets and run history
	// retain their catalog identity. The submitted snapshot never saves/publishes.
	value, err := s.modules.Catalog.Get(r.PathValue("id"), &version)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	workflowType, source := "RunloomChildTestWorkflow", "child_test"
	if body.Definition != nil {
		value = *body.Definition
		value.Version = 0
		value, err = s.hydratePrompts(r, value)
		if err != nil {
			writeError(w, 422, err)
			return
		}
		value, err = definition.Normalize(value)
		if err != nil {
			writeError(w, 422, err)
			return
		}
		workflowType, source = "RunloomDraftTestWorkflow", "draft_test"
	} else if value.Kind != "child" {
		writeError(w, 422, fmt.Errorf("Studio child tests require kind child"))
		return
	}
	if body.Input == nil {
		body.Input = map[string]any{}
	}
	if err := definition.ValidateInput(value.InputSchema, body.Input); err != nil {
		writeError(w, 422, err)
		return
	}
	input, err := s.prepareRunSnapshots(r, value, body.Input)
	if err != nil {
		writeError(w, 422, err)
		return
	}
	input.IsTest, input.Source = true, source
	id := input.TeamID + "-test-" + uuid.NewString()
	tagRun(&input, id, source, true)
	run, err := s.temporal.ExecuteWorkflow(r.Context(), client.StartWorkflowOptions{ID: id, TaskQueue: s.taskQueue, TypedSearchAttributes: input.Metadata.SearchAttributes()}, model.WorkflowType(workflowType, input), input)
	if err != nil {
		writeError(w, 502, fmt.Errorf("Temporal rejected the workflow test: %w", err))
		return
	}
	if err := s.recordStarted(store.RunRecord{Metadata: input.Metadata, WorkflowID: run.GetID(), TemporalRunID: run.GetRunID(), DefinitionID: value.ID, DefinitionVersion: value.Version, TriggerType: source, Input: body.Input, TemporalStatus: "running"}, requestActor(r, "studio-user")); err != nil {
		_ = s.temporal.CancelWorkflow(r.Context(), run.GetID(), run.GetRunID())
		writeError(w, 503, fmt.Errorf("persist workflow test start: %w", err))
		return
	}
	writeJSON(w, 201, map[string]any{"workflow_id": run.GetID(), "run_id": run.GetRunID(), "definition_id": value.ID, "definition_version": value.Version, "is_test": true})
}
