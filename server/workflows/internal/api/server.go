package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/google/uuid"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
)

type Server struct {
	orgID     string
	namespace string
	modules   store.Modules
	temporal  client.Client
	taskQueue string
	mux       *http.ServeMux
	done      chan struct{}
	closeOnce sync.Once
}

type runRequest struct {
	DefinitionID string         `json:"definition_id"`
	Version      *int           `json:"version,omitempty"`
	Input        map[string]any `json:"input"`
	WorkflowID   string         `json:"workflow_id,omitempty"`
}

type payloadRequest struct {
	EventID string `json:"event_id,omitempty"`
	Payload any    `json:"payload"`
	Actor   string `json:"actor"`
}

type restoreCommitRequest struct {
	Message string `json:"message"`
}

func New(definitions store.Backend, temporalClient client.Client, taskQueue string) *Server {
	return NewModules(store.Bind(definitions), temporalClient, taskQueue)
}

func NewModules(modules store.Modules, temporalClient client.Client, taskQueue string) *Server {
	server := &Server{modules: modules, temporal: temporalClient, taskQueue: taskQueue, mux: http.NewServeMux(), done: make(chan struct{})}
	server.routes()
	if temporalClient != nil {
		go server.reconcileRuns()
	}
	return server
}

func (s *Server) Handler() http.Handler {
	return withSecurityHeaders(s.audited(s.mux))
}

func (s *Server) Close() { s.closeOnce.Do(func() { close(s.done) }) }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/v1/studio/authoring/workflows", s.authoringContext)
	s.mux.HandleFunc("POST /api/v1/studio/authoring/workflows", s.applyAuthoring)
	s.mux.HandleFunc("GET /api/v1/studio/definitions/{id}/authoring", s.authoringContext)
	s.mux.HandleFunc("POST /api/v1/studio/definitions/{id}/authoring", s.applyAuthoring)
	s.mux.HandleFunc("GET /api/v1/studio/prompts", s.listPrompts)
	s.mux.HandleFunc("GET /api/v1/studio/prompts/{id}", s.getPrompt)
	s.mux.HandleFunc("GET /api/v1/studio/prompts/{id}/history", s.promptHistory)
	s.mux.HandleFunc("POST /api/v1/studio/prompts/{id}/{action}", s.savePrompt)
	s.mux.HandleFunc("POST /api/v1/studio/prompts/preview", s.previewPrompt)
	s.mux.HandleFunc("POST /api/v1/studio/prompts/test", s.testPrompt)
	s.mux.HandleFunc("POST /api/v1/studio/variables", s.variableHints)
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	s.mux.HandleFunc("GET /{$}", s.index)
	s.mux.HandleFunc("GET /api/v1/health", s.health)
	s.mux.HandleFunc("GET /api/v1/studio/catalog", s.catalog)
	s.mux.HandleFunc("POST /api/v1/studio/code/test", s.testCode)
	s.mux.HandleFunc("GET /api/v1/studio/schedules", s.listSchedules)
	s.mux.HandleFunc("POST /api/v1/studio/schedules", s.createSchedule)
	s.mux.HandleFunc("GET /api/v1/studio/schedules/{schedule}", s.getSchedule)
	s.mux.HandleFunc("POST /api/v1/studio/schedules/{schedule}/{action}", s.changeSchedule)
	s.mux.HandleFunc("DELETE /api/v1/studio/schedules/{schedule}", s.changeSchedule)
	s.mux.HandleFunc("GET /api/v1/studio/projects", s.listProjects)
	s.mux.HandleFunc("POST /api/v1/studio/projects", s.createProject)
	s.mux.HandleFunc("DELETE /api/v1/studio/projects/{project}", s.deleteProject)
	s.mux.HandleFunc("POST /api/v1/studio/projects/{project}/rename", s.renameProject)
	s.mux.HandleFunc("POST /api/v1/studio/definitions/{id}/project", s.moveDefinitionProject)
	s.mux.HandleFunc("POST /api/v1/studio/definitions/validate", s.validateDefinition)
	s.mux.HandleFunc("POST /api/v1/studio/definitions", s.saveDefinition)
	s.mux.HandleFunc("GET /api/v1/studio/definitions", s.listDefinitions)
	s.mux.HandleFunc("GET /api/v1/studio/definitions/{id}", s.getDefinition)
	s.mux.HandleFunc("DELETE /api/v1/studio/definitions/{id}", s.deleteDraftWorkflow)
	for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
		s.mux.HandleFunc(method+" /api/v1/studio/definitions/{id}/edit-lock", s.editLock)
	}
	s.mux.HandleFunc("GET /api/v1/studio/definitions/{id}/history", s.definitionHistory)
	s.mux.HandleFunc("GET /api/v1/studio/definitions/{id}/commits/{commit}", s.getDefinitionCommit)
	s.mux.HandleFunc("POST /api/v1/studio/definitions/{id}/commits/{commit}/restore", s.restoreDefinitionCommit)
	s.mux.HandleFunc("POST /api/v1/studio/definitions/{id}/publish", s.publishDefinition)
	s.mux.HandleFunc("POST /api/v1/studio/runs", s.startRun)
	s.mux.HandleFunc("POST /api/v1/studio/definitions/{id}/test", s.testWorkflow)
	s.mux.HandleFunc("GET /api/v1/studio/runs", s.listRuns)
	s.mux.HandleFunc("GET /api/v1/studio/audit", s.listAudit)
	s.mux.HandleFunc("GET /api/v1/studio/runs/{id}", s.getRun)
	s.mux.HandleFunc("GET /api/v1/studio/runs/{id}/history", s.getRunHistory)
	s.mux.HandleFunc("GET /api/v1/studio/runs/{id}/llm-usage", s.runLLMUsage)
	s.mux.HandleFunc("POST /api/v1/studio/runs/{id}/approvals", s.approveRun)
	s.mux.HandleFunc("POST /api/v1/studio/runs/{id}/callbacks", s.callbackRun)
	s.mux.HandleFunc("POST /api/v1/studio/callbacks/{id}/{step}", s.publicCallback)
	s.mux.HandleFunc("POST /api/v1/studio/runs/{id}/signals/{signal}", s.signalRun)
	s.mux.HandleFunc("POST /api/v1/studio/runs/{id}/resume", s.resumeRun)
	s.mux.HandleFunc("POST /api/v1/studio/runs/{id}/cancel", s.cancelRun)
	s.mux.HandleFunc("POST /api/v1/studio/webhooks/{definition}/{path}", s.webhookRun)
}

func (s *Server) index(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"service": "Runloom Go API",
		"studio":  "http://localhost:4200",
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	_, err := s.temporal.CheckHealth(ctx, &client.CheckHealthRequest{})
	databaseOK := s.modules.Lifecycle.Health(ctx)
	healthy := err == nil && databaseOK
	code := http.StatusOK
	if !healthy {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]any{"status": status(healthy), "temporal": err == nil, "database": databaseOK, "database_driver": s.modules.Lifecycle.Driver()})
}

func (s *Server) catalog(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"integrations": definition.IntegrationCatalog(),
		"activities": []map[string]string{
			{"name": "runloom.echo", "description": "Return its input unchanged"},
			{"name": "runloom.compose", "description": "Return a resolved message"},
			{"name": "runloom.sum", "description": "Sum an array of numbers"},
			{"name": "runloom.llm", "description": "Call the configured OpenAI-compatible language model provider"},
		},
		"step_types":     []string{"transform", "collection", "merge", "foreach", "fail", "integration", "database", "http", "end", "code", "email", "activity", "llm", "decision", "subworkflow", "wait", "callback", "approval", "pause", "signal", "condition", "parallel"},
		"trigger_types":  []string{"manual", "webhook"},
		"workflow_kinds": []string{"main", "child"},
	})
}

func (s *Server) validateDefinition(w http.ResponseWriter, r *http.Request) {
	value, err := decodeDefinition(w, r)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	// Keep the original draft for all-node diagnostics, even if hydration or
	// strict normalization fails partway through the definition.
	raw, _ := json.Marshal(value)
	var original model.Definition
	_ = json.Unmarshal(raw, &original)
	invalid := func(err error) {
		issues := definition.NodeIssues(original, func(step model.Step) (model.Step, error) {
			probe := model.Definition{SchemaVersion: "1.0", ID: original.ID, Name: "Node diagnostics", Trigger: model.Trigger{Type: "manual"}, Steps: []model.Step{step}}
			resolved, err := s.hydratePrompts(r, probe)
			if err != nil {
				return step, err
			}
			if step.Type == "subworkflow" {
				_, err = definition.ResolveDependencies(resolved, s.modules.Catalog.Get)
			}
			return resolved.Steps[0], err
		})
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"detail": err.Error(), "issues": issues})
	}
	value, err = s.hydratePrompts(r, value)
	if err != nil {
		invalid(err)
		return
	}
	value, err = definition.Normalize(value)
	if err != nil {
		invalid(err)
		return
	}
	if _, err := definition.ResolveDependencies(value, s.modules.Catalog.Get); err != nil {
		invalid(err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": true, "definition": value})
}

func (s *Server) saveDefinition(w http.ResponseWriter, r *http.Request) {
	value, err := decodeDefinition(w, r)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	value, err = definition.Draft(value)
	if err != nil {
		writeError(w, 422, err)
		return
	}
	finish, ok := s.guardDefinitionWrite(w, r, value.ID)
	if !ok {
		return
	}
	defer finish()
	// Prompt/dependency errors must not prevent saving work in progress. Resolve
	// known immutable prompts on a copy so a partial failure cannot alter the draft.
	raw, _ := json.Marshal(value)
	var candidate model.Definition
	_ = json.Unmarshal(raw, &candidate)
	if resolved, resolveErr := s.hydratePrompts(r, candidate); resolveErr == nil {
		value = resolved
	}
	actor := requestActor(r, "api-user")
	catalog, err := s.modules.Projects.ProjectCatalog(r.Context())
	if err != nil {
		writeProjectError(w, err)
		return
	}
	projectID := r.Header.Get("X-Runloom-Project")
	if projectID == "" {
		projectID = catalog.WorkflowProject(value.ID)
	}
	if _, ok := catalog.Projects[projectID]; !ok {
		writeProjectError(w, store.ErrProjectNotFound)
		return
	}
	value, commit, err := s.modules.Catalog.CommitDraft(value, store.CommitMeta{Actor: actor, Message: requestCommitMessage(r, "Updated workflow")})
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	if catalog.WorkflowProject(value.ID) != projectID {
		if err := s.modules.Projects.AssignProject(r.Context(), value.ID, projectID); err != nil {
			writeError(w, http.StatusServiceUnavailable, fmt.Errorf("draft saved but project assignment failed; retry save"))
			return
		}
		s.audit(store.AuditEvent{DefinitionID: value.ID, EventType: "definition.project_moved", Actor: actor, Payload: map[string]string{"from_project_id": catalog.WorkflowProject(value.ID), "project_id": projectID}})
	}
	s.audit(store.AuditEvent{DefinitionID: value.ID, EventType: "definition.draft_saved", Actor: actor, Payload: map[string]any{"name": value.Name, "commit_id": commit.CommitID, "sequence": commit.Sequence, "message": commit.Message}})
	writeJSON(w, http.StatusOK, value)
}

func (s *Server) listDefinitions(w http.ResponseWriter, r *http.Request) {
	items, err := s.modules.Catalog.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	catalog, err := s.modules.Projects.ProjectCatalog(r.Context())
	if err != nil {
		writeProjectError(w, err)
		return
	}
	filter := r.URL.Query().Get("project_id")
	if filter != "" {
		if _, ok := catalog.Projects[filter]; !ok {
			writeProjectError(w, store.ErrProjectNotFound)
			return
		}
	}
	filtered := make([]store.Summary, 0, len(items))
	for _, item := range items {
		item.ProjectID = catalog.WorkflowProject(item.ID)
		if filter == "" || item.ProjectID == filter {
			filtered = append(filtered, item)
		}
	}
	writeJSON(w, http.StatusOK, filtered)
}

func (s *Server) getDefinition(w http.ResponseWriter, r *http.Request) {
	var version *int
	if raw := r.URL.Query().Get("version"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("version must be a non-negative integer"))
			return
		}
		version = &parsed
	}
	value, err := s.modules.Catalog.Get(r.PathValue("id"), version)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s *Server) definitionHistory(w http.ResponseWriter, r *http.Request) {
	limit, ok := parseLimit(w, r, 100, 500)
	if !ok {
		return
	}
	definitions, err := s.modules.Catalog.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	var summary *store.Summary
	for index := range definitions {
		if definitions[index].ID == r.PathValue("id") {
			summary = &definitions[index]
			break
		}
	}
	if summary == nil {
		writeError(w, http.StatusNotFound, store.ErrNotFound)
		return
	}
	commits, err := s.modules.Catalog.ListCommits(summary.ID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"definition_id": summary.ID, "latest_version": summary.LatestVersion, "published_versions": summary.PublishedVersions, "commits": commits})
}

func (s *Server) getDefinitionCommit(w http.ResponseWriter, r *http.Request) {
	value, err := s.modules.Catalog.GetCommit(r.PathValue("id"), r.PathValue("commit"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s *Server) restoreDefinitionCommit(w http.ResponseWriter, r *http.Request) {
	finish, ok := s.guardDefinitionWrite(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	defer finish()
	var body restoreCommitRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	if len(body.Message) > 500 {
		writeError(w, http.StatusUnprocessableEntity, fmt.Errorf("message must be at most 500 characters"))
		return
	}
	actor := requestActor(r, "api-user")
	value, commit, err := s.modules.Catalog.RestoreCommit(r.PathValue("id"), r.PathValue("commit"), store.CommitMeta{Actor: actor, Message: body.Message})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.audit(store.AuditEvent{DefinitionID: value.ID, EventType: "definition.commit_restored", Actor: actor, Payload: map[string]any{"commit_id": commit.CommitID, "restored_from_commit_id": r.PathValue("commit"), "sequence": commit.Sequence}})
	writeJSON(w, http.StatusOK, value)
}

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	limit, ok := parseLimit(w, r, 50, 200)
	if !ok {
		return
	}
	filter := store.RunFilter{Limit: limit, Search: strings.TrimSpace(r.URL.Query().Get("search")), TagKey: r.URL.Query().Get("tag_key"), TagValue: r.URL.Query().Get("tag_value")}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		offset, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, errors.New("offset must be an integer"))
			return
		}
		filter.Offset = offset
	}
	if err := filter.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	values, err := s.modules.Runs.SearchRuns(ctx, filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if r.URL.Query().Get("include_usage") == "true" {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		for i := range values {
			if i >= 20 {
				break
			}
			if summary, err := usageSummary(ctx, s.modules.Metering, values[i]); err == nil {
				values[i].LLMCost = &summary
			}
		}
	}
	writeJSON(w, http.StatusOK, values)
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	limit, ok := parseLimit(w, r, 200, 1000)
	if !ok {
		return
	}
	values, err := s.modules.Audits.ListAudit(r.URL.Query().Get("workflow_id"), r.URL.Query().Get("definition_id"), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, values)
}

func (s *Server) publishDefinition(w http.ResponseWriter, r *http.Request) {
	finish, ok := s.guardDefinitionWrite(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	defer finish()
	version := 0
	draft, err := s.modules.Catalog.Get(r.PathValue("id"), &version)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if _, err := definition.ResolveDependencies(draft, s.modules.Catalog.Get); err != nil {
		writeError(w, 422, err)
		return
	}
	// Drafts may contain unresolved/invalid library references. Publication must
	// verify team ownership even when the submitted snapshot has a plausible hash.
	if _, err := s.hydratePrompts(r, draft); err != nil {
		writeError(w, 422, err)
		return
	}
	actor := requestActor(r, "api-user")
	value, err := s.modules.Catalog.PublishWithCommit(r.PathValue("id"), store.CommitMeta{Actor: actor, Message: requestCommitMessage(r, "")})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.audit(store.AuditEvent{DefinitionID: value.ID, EventType: "definition.published", Actor: actor, Payload: map[string]any{"version": value.Version}})
	writeJSON(w, http.StatusOK, value)
}

func (s *Server) startRun(w http.ResponseWriter, r *http.Request) {
	var body runRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	if body.Version != nil && *body.Version < 1 {
		writeError(w, http.StatusUnprocessableEntity, fmt.Errorf("version must be at least 1"))
		return
	}
	if body.Input == nil {
		body.Input = map[string]any{}
	}
	value, err := s.modules.Catalog.Get(body.DefinitionID, body.Version)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if err := definition.ValidateInput(value.InputSchema, body.Input); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	workflowID := body.WorkflowID
	if workflowID == "" {
		workflowID = uuid.NewString()
	}
	workflowID = r.Header.Get("X-Runloom-Team") + "-" + workflowID
	if len(workflowID) > 200 {
		writeError(w, http.StatusUnprocessableEntity, fmt.Errorf("workflow_id must be at most 200 characters"))
		return
	}
	runInput, err := s.prepareRun(r, value, body.Input)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	tagRun(&runInput, workflowID, "manual", false)
	run, err := s.temporal.ExecuteWorkflow(r.Context(), client.StartWorkflowOptions{ID: workflowID, TaskQueue: s.taskQueue, TypedSearchAttributes: runInput.Metadata.SearchAttributes()}, model.WorkflowType("RunloomWorkflow", runInput), runInput)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Errorf("Temporal rejected the run: %w", err))
		return
	}
	if err := s.recordStarted(store.RunRecord{Metadata: runInput.Metadata, WorkflowID: run.GetID(), TemporalRunID: run.GetRunID(), DefinitionID: value.ID, DefinitionVersion: value.Version, TriggerType: "manual", Input: body.Input, TemporalStatus: "running"}, requestActor(r, "api-user")); err != nil {
		_ = s.temporal.CancelWorkflow(r.Context(), run.GetID(), run.GetRunID())
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("persist run start: %w", err))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"workflow_id": run.GetID(), "run_id": run.GetRunID(), "definition_id": value.ID, "definition_version": value.Version})
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	observed := time.Now().UTC()
	temporalStatus, state, err := s.snapshotRun(r.Context(), id)
	if err != nil {
		writeTemporalError(w, err)
		return
	}
	if changed, updateErr := s.modules.Runs.ObserveRun(r.Context(), id, temporalStatus, state, observed); updateErr == nil && changed {
		s.audit(store.AuditEvent{WorkflowID: id, EventType: "workflow.status_changed", Actor: "system", Payload: map[string]any{"temporal_status": temporalStatus, "state": state}})
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflow_id": id, "temporal_status": temporalStatus, "state": state})
}

func (s *Server) snapshotRun(ctx context.Context, id string) (string, *model.RunState, error) {
	description, err := s.temporal.DescribeWorkflowExecution(ctx, id, "")
	if err != nil {
		return "", nil, err
	}
	var state *model.RunState
	if encoded, queryErr := s.temporal.QueryWorkflow(ctx, id, "", "status"); queryErr == nil {
		var value model.RunState
		if decodeErr := encoded.Get(&value); decodeErr == nil {
			state = &value
		}
	}
	temporalStatus := strings.ToLower(strings.TrimPrefix(description.WorkflowExecutionInfo.Status.String(), "WORKFLOW_EXECUTION_STATUS_"))
	return temporalStatus, state, nil
}

func (s *Server) reconcileRuns() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	after := ""
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		runs, err := s.modules.Runs.PendingRuns(ctx, after, 100)
		cancel()
		if err != nil {
			continue
		}
		if len(runs) < 100 {
			after = ""
		} else {
			after = runs[len(runs)-1].WorkflowID
		}
		for _, run := range runs {
			if runStatusFinished(run.TemporalStatus) {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			observed := time.Now().UTC()
			status, state, snapshotErr := s.snapshotRun(ctx, run.WorkflowID)
			cancel()
			if snapshotErr != nil {
				continue
			}
			if changed, updateErr := s.modules.Runs.ObserveRun(context.Background(), run.WorkflowID, status, state, observed); updateErr == nil && changed {
				s.audit(store.AuditEvent{WorkflowID: run.WorkflowID, DefinitionID: run.DefinitionID, EventType: "workflow.status_changed", Actor: "system", Payload: map[string]any{"temporal_status": status, "state": state}})
			}
		}
	}
}

func (s *Server) getRunHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	record, err := s.modules.Runs.GetRun(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	events, err := s.modules.Audits.ListAudit(id, "", 1000)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	response := map[string]any{"run": record, "events": events}
	if r.URL.Query().Get("trace") == "true" && record.TemporalRunID != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if encoded, err := s.temporal.QueryWorkflow(ctx, id, record.TemporalRunID, "execution_trace"); err == nil {
			var trace model.ExecutionTrace
			if encoded.Get(&trace) == nil && trace.Nodes != nil {
				response["trace"] = trace
			}
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) approveRun(w http.ResponseWriter, r *http.Request) {
	var decision model.ApprovalDecision
	if err := decodeJSON(w, r, &decision); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	if decision.StepID == "" || len(decision.Comment) > 2000 || len(decision.Actor) > 200 {
		writeError(w, http.StatusUnprocessableEntity, fmt.Errorf("approval fields are invalid"))
		return
	}
	decision.Actor = requestActor(r, "api-user")
	if err := s.temporal.SignalWorkflow(r.Context(), r.PathValue("id"), "", "approval", decision); err != nil {
		writeTemporalError(w, err)
		return
	}
	s.audit(store.AuditEvent{WorkflowID: r.PathValue("id"), EventType: "approval.received", StepID: decision.StepID, Actor: requestActor(r, "api-user"), Payload: map[string]any{"approved": decision.Approved, "comment": decision.Comment}})
	writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
}

func (s *Server) callbackRun(w http.ResponseWriter, r *http.Request) {
	s.signalEvent(w, r, "callback")
}

func (s *Server) publicCallback(w http.ResponseWriter, r *http.Request) {
	var body payloadRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	if len(body.Actor) > 200 {
		writeError(w, http.StatusUnprocessableEntity, fmt.Errorf("actor is invalid"))
		return
	}
	event := model.WorkflowEvent{StepID: r.PathValue("step"), Payload: body.Payload, Actor: requestActor(r, "api-user")}
	if err := s.temporal.SignalWorkflow(r.Context(), r.PathValue("id"), "", "callback", event); err != nil {
		writeTemporalError(w, err)
		return
	}
	s.audit(store.AuditEvent{WorkflowID: r.PathValue("id"), EventType: "callback.received", StepID: event.StepID, Actor: actor(event.Actor, requestActor(r, "api-user")), Payload: event.Payload})
	writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
}

func (s *Server) signalRun(w http.ResponseWriter, r *http.Request) {
	var body payloadRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	if len(body.Actor) > 200 {
		writeError(w, http.StatusUnprocessableEntity, fmt.Errorf("actor is invalid"))
		return
	}
	if body.EventID != "" {
		id, err := uuid.Parse(body.EventID)
		if err != nil {
			writeError(w, 422, fmt.Errorf("event_id must be a UUID"))
			return
		}
		body.EventID = id.String()
	}
	// Optional live prevalidation. The workflow validates native deliveries too.
	if body.EventID != "" {
		if encoded, err := s.temporal.QueryWorkflow(r.Context(), r.PathValue("id"), "", "status"); err == nil {
			var state model.RunState
			if encoded.Get(&state) == nil {
				if handler, known := state.SignalHandlers[r.PathValue("signal")]; known {
					if !handler.Accepting {
						writeError(w, 409, fmt.Errorf("signal handler is closed"))
						return
					}
					total, pending := 0, 0
					for _, value := range state.SignalHandlers {
						total += value.Received
						pending += value.Pending
					}
					if pending >= 100 || total >= 1000 {
						writeError(w, 429, fmt.Errorf("signal handler queue or execution limit reached"))
						return
					}
					raw, payloadErr := definition.CanonicalDataJSON(body.Payload)
					if payloadErr != nil {
						writeError(w, 422, payloadErr)
						return
					}
					if len(raw) > 64*1024 {
						writeError(w, 422, fmt.Errorf("handler payload exceeds 64 KiB"))
						return
					}
					if len(handler.InputSchema) > 0 {
						if err := definition.ValidateInput(handler.InputSchema, body.Payload); err != nil {
							writeError(w, 422, err)
							return
						}
					}
				}
			}
		}
	}
	event := model.WorkflowEvent{EventID: body.EventID, SignalName: r.PathValue("signal"), Payload: body.Payload, Actor: requestActor(r, "api-user")}
	if err := s.temporal.SignalWorkflow(r.Context(), r.PathValue("id"), "", "signal", event); err != nil {
		writeTemporalError(w, err)
		return
	}
	s.audit(store.AuditEvent{WorkflowID: r.PathValue("id"), EventType: "signal.received", StepID: event.SignalName, Actor: actor(event.Actor, requestActor(r, "api-user")), Payload: event.Payload})
	writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
}

func (s *Server) resumeRun(w http.ResponseWriter, r *http.Request) {
	s.signalEvent(w, r, "resume")
}

func (s *Server) signalEvent(w http.ResponseWriter, r *http.Request, signalName string) {
	var event model.WorkflowEvent
	if err := decodeJSON(w, r, &event); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	if event.StepID == "" || len(event.StepID) > 64 || len(event.Actor) > 200 {
		writeError(w, http.StatusUnprocessableEntity, fmt.Errorf("event fields are invalid"))
		return
	}
	event.Actor = requestActor(r, "api-user")
	if err := s.temporal.SignalWorkflow(r.Context(), r.PathValue("id"), "", signalName, event); err != nil {
		writeTemporalError(w, err)
		return
	}
	eventType := signalName + ".received"
	if signalName == "resume" {
		eventType = "workflow.resume_requested"
	} else if signalName == "callback" {
		eventType = "callback.received"
	}
	s.audit(store.AuditEvent{WorkflowID: r.PathValue("id"), EventType: eventType, StepID: event.StepID, Actor: actor(event.Actor, requestActor(r, "api-user")), Payload: event.Payload})
	writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
}

func (s *Server) cancelRun(w http.ResponseWriter, r *http.Request) {
	if err := s.temporal.CancelWorkflow(r.Context(), r.PathValue("id"), ""); err != nil {
		writeTemporalError(w, err)
		return
	}
	s.audit(store.AuditEvent{WorkflowID: r.PathValue("id"), EventType: "workflow.cancel_requested", Actor: requestActor(r, "api-user"), Payload: map[string]any{}})
	writeJSON(w, http.StatusOK, map[string]bool{"cancelled": true})
}

func (s *Server) webhookRun(w http.ResponseWriter, r *http.Request) {
	var input map[string]any
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	value, err := s.modules.Catalog.Get(r.PathValue("definition"), nil)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if value.Kind == "child" {
		writeError(w, http.StatusUnprocessableEntity, fmt.Errorf("child workflows can only be started by a parent workflow"))
		return
	}
	if value.Trigger.Type != "webhook" || value.Trigger.Path != r.PathValue("path") {
		writeError(w, http.StatusNotFound, fmt.Errorf("webhook not found"))
		return
	}
	if err := definition.ValidateInput(value.InputSchema, input); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	workflowID := r.Header.Get("X-Runloom-Team") + "-webhook-" + uuid.NewString()
	runInput, err := s.prepareRun(r, value, input)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	tagRun(&runInput, workflowID, "webhook", false)
	run, err := s.temporal.ExecuteWorkflow(r.Context(), client.StartWorkflowOptions{ID: workflowID, TaskQueue: s.taskQueue, TypedSearchAttributes: runInput.Metadata.SearchAttributes()}, model.WorkflowType("RunloomWorkflow", runInput), runInput)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Errorf("Temporal rejected the webhook run: %w", err))
		return
	}
	if err := s.recordStarted(store.RunRecord{Metadata: runInput.Metadata, WorkflowID: run.GetID(), TemporalRunID: run.GetRunID(), DefinitionID: value.ID, DefinitionVersion: value.Version, TriggerType: "webhook", Input: input, TemporalStatus: "running"}, requestActor(r, "webhook")); err != nil {
		_ = s.temporal.CancelWorkflow(r.Context(), run.GetID(), run.GetRunID())
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("persist run start: %w", err))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"accepted": true, "workflow_id": run.GetID(), "run_id": run.GetRunID(), "definition_id": value.ID, "definition_version": value.Version})
}

func (s *Server) recordStarted(value store.RunRecord, auditActor string) error {
	if _, err := s.modules.Runs.RecordRunStart(value); err != nil {
		return err
	}
	_, err := s.modules.Audits.AppendAudit(store.AuditEvent{WorkflowID: value.WorkflowID, DefinitionID: value.DefinitionID, EventType: "workflow.started", Actor: auditActor, Payload: map[string]any{"definition_version": value.DefinitionVersion, "trigger_type": value.TriggerType}})
	return err
}

func (s *Server) audit(value store.AuditEvent) {
	_, _ = s.modules.Audits.AppendAudit(value)
}

func decodeDefinition(w http.ResponseWriter, r *http.Request) (model.Definition, error) {
	var value model.Definition
	err := decodeJSON(w, r, &value)
	return value, err
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, definition.MaxDefinitionBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("request must contain one JSON value")
	}
	return nil
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeError(w, http.StatusUnprocessableEntity, err)
}

func writeTemporalError(w http.ResponseWriter, err error) {
	var notFound *serviceerror.NotFound
	if errors.As(err, &notFound) {
		writeError(w, http.StatusNotFound, fmt.Errorf("run not found"))
		return
	}
	writeError(w, http.StatusBadGateway, err)
}

func writeError(w http.ResponseWriter, code int, err error) {
	if code >= 500 {
		err = fmt.Errorf("service operation failed; retry or contact an administrator")
	}
	writeJSON(w, code, map[string]string{"detail": err.Error()})
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func status(healthy bool) string {
	if healthy {
		return "ok"
	}
	return "degraded"
}

func parseLimit(w http.ResponseWriter, r *http.Request, fallback, maximum int) (int, bool) {
	limit := fallback
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > maximum {
			writeError(w, http.StatusUnprocessableEntity, fmt.Errorf("limit must be from 1 through %d", maximum))
			return 0, false
		}
		limit = value
	}
	return limit, true
}

func requestActor(r *http.Request, fallback string) string {
	value := r.Header.Get("X-Runloom-Actor")
	if value == "" {
		return fallback
	}
	if len(value) > 200 {
		return value[:200]
	}
	return value
}

func requestCommitMessage(r *http.Request, fallback string) string {
	value := r.Header.Get("X-Runloom-Commit-Message")
	if value == "" {
		return fallback
	}
	if len(value) > 500 {
		return value[:500]
	}
	return value
}

func actor(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func runStatusFinished(value string) bool {
	switch value {
	case "completed", "failed", "canceled", "cancelled", "terminated", "timed_out":
		return true
	default:
		return false
	}
}

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
