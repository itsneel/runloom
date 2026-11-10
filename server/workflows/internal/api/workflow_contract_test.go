package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/stretchr/testify/require"
	enumspb "go.temporal.io/api/enums/v1"
	workflowpb "go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
)

type documentedRun struct {
	client.WorkflowRun
	id string
}

func (r documentedRun) GetID() string    { return r.id }
func (r documentedRun) GetRunID() string { return "22222222-2222-4222-8222-222222222222" }

type documentedClient struct {
	client.Client
	id          string
	input       model.RunInput
	status      enumspb.WorkflowExecutionStatus
	cancelledID string
	resumedID   string
	resumeEvent model.WorkflowEvent
}

func (c *documentedClient) ExecuteWorkflow(_ context.Context, options client.StartWorkflowOptions, _ any, args ...any) (client.WorkflowRun, error) {
	c.id, c.input = options.ID, args[0].(model.RunInput)
	return documentedRun{id: c.id}, nil
}
func (c *documentedClient) DescribeWorkflowExecution(context.Context, string, string) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
	return &workflowservice.DescribeWorkflowExecutionResponse{WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{Status: c.status}}, nil
}
func (c *documentedClient) QueryWorkflow(context.Context, string, string, string, ...any) (converter.EncodedValue, error) {
	return nil, errors.New("worker query unavailable")
}
func (c *documentedClient) CancelWorkflow(_ context.Context, id, _ string) error {
	c.cancelledID = id
	return nil // Acknowledgement alone does not close the execution.
}

func (c *documentedClient) SignalWorkflow(_ context.Context, id, _ string, signal string, arg any) error {
	if signal == "resume" {
		c.resumedID, c.resumeEvent = id, arg.(model.WorkflowEvent)
	}
	return nil
}

func TestDocumentedWorkflowAPI(t *testing.T) {
	doc, err := os.ReadFile("../../testdata/docs/workflow-api.md")
	require.NoError(t, err)
	examples := regexp.MustCompile("(?s)```json\\n(.*?)\\n```").FindAllSubmatch(doc, -1)
	require.Len(t, examples, 6)
	for _, example := range examples {
		require.True(t, json.Valid(example[1]), "invalid documented JSON")
	}

	backend, err := store.New(t.TempDir())
	require.NoError(t, err)
	defer backend.Close(context.Background())
	fixture, err := os.ReadFile("../../testdata/examples/employee-onboarding.json")
	require.NoError(t, err)
	var definition model.Definition
	require.NoError(t, json.Unmarshal(fixture, &definition))
	_, err = backend.SaveDraft(definition)
	require.NoError(t, err)
	_, err = backend.Publish(definition.ID)
	require.NoError(t, err)
	c := &documentedClient{status: enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING}
	// Exercise the HTTP handlers without a reconciler or a live Temporal server.
	s := &Server{modules: store.Bind(backend), temporal: c, taskQueue: "test", mux: http.NewServeMux()}
	s.routes()
	call := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("X-Runloom-Team", "11111111-1111-4111-8111-111111111111")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	started := call("POST", "/api/v1/studio/runs", string(examples[0][1]))
	require.Equal(t, 201, started.Code, started.Body.String())
	var response map[string]any
	require.NoError(t, json.Unmarshal(started.Body.Bytes(), &response))
	require.Equal(t, c.id, response["workflow_id"])
	require.True(t, strings.HasPrefix(c.id, "11111111-1111-4111-8111-111111111111-"))
	require.Equal(t, "22222222-2222-4222-8222-222222222222", response["run_id"])
	require.Equal(t, "employee-onboarding", response["definition_id"])
	require.Equal(t, float64(1), response["definition_version"])
	require.Equal(t, "Alex", c.input.Input["employee_name"])
	resumed := call("POST", "/api/v1/studio/runs/"+c.id+"/resume", `{"step_id":"review-pause","payload":{"note":"Ready to continue"}}`)
	require.Equal(t, 200, resumed.Code)
	require.JSONEq(t, string(examples[4][1]), resumed.Body.String())
	require.Equal(t, c.id, c.resumedID)
	require.Equal(t, "review-pause", c.resumeEvent.StepID)

	cancel := call("POST", "/api/v1/studio/runs/"+c.id+"/cancel", "")
	require.Equal(t, 200, cancel.Code)
	require.JSONEq(t, string(examples[3][1]), cancel.Body.String())
	require.Equal(t, c.id, c.cancelledID)
	for _, tc := range []struct {
		status enumspb.WorkflowExecutionStatus
		wire   string
	}{
		{enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING, "running"},
		{enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED, "canceled"},
		{enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT, "timedout"},
		{enumspb.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW, "continuedasnew"},
	} {
		c.status = tc.status
		status := call("GET", "/api/v1/studio/runs/"+c.id, "")
		require.Equal(t, 200, status.Code)
		var snapshot map[string]any
		require.NoError(t, json.Unmarshal(status.Body.Bytes(), &snapshot))
		require.Equal(t, tc.wire, snapshot["temporal_status"])
		require.Contains(t, snapshot, "state")
		require.Nil(t, snapshot["state"])
	}
}

// Execution must not depend on the authoring catalog being available.
type unavailableProjectCatalog struct {
	store.Backend
	reads int
}

func (s *unavailableProjectCatalog) ProjectCatalog(context.Context) (store.ProjectCatalog, error) {
	s.reads++
	return store.ProjectCatalog{}, errors.New("authoring project catalog unavailable")
}

func TestExecutionIndependentOfAuthoringProjects(t *testing.T) {
	ctx := context.Background()
	backend, err := store.New(t.TempDir())
	require.NoError(t, err)
	defer backend.Close(ctx)
	definition := model.Definition{
		SchemaVersion: "1.0", ID: "project-independent", Name: "Project independent execution",
		Trigger: model.Trigger{Type: "webhook", Path: "start", Method: "POST"},
		Steps:   []model.Step{{ID: "echo", Type: "activity", Activity: "runloom.echo"}},
	}
	_, err = backend.SaveDraft(definition)
	require.NoError(t, err)
	_, err = backend.Publish(definition.ID)
	require.NoError(t, err)
	published, err := backend.Get(definition.ID, nil)
	require.NoError(t, err)
	authoringProject, err := backend.CreateProject(ctx, "Authoring")
	require.NoError(t, err)
	require.NoError(t, backend.AssignProject(ctx, definition.ID, authoringProject.ID))
	c := &documentedClient{status: enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING}
	s := &Server{modules: store.Bind(backend), temporal: c, taskQueue: "test", mux: http.NewServeMux()}
	s.routes()
	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("X-Runloom-Team", "execution-team")
		// Deliberately no project header or project ID in execution requests.
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	startBody := `{"definition_id":"project-independent","version":1,"input":{}}`
	started := call("POST", "/api/v1/studio/runs", startBody)
	require.Equal(t, http.StatusCreated, started.Code, started.Body.String())
	firstRunID := c.id
	require.Equal(t, published, c.input.Definition)

	_, err = backend.RenameProject(ctx, authoringProject.ID, "Renamed authoring folder")
	require.NoError(t, err)
	otherProject, err := backend.CreateProject(ctx, "Another authoring folder")
	require.NoError(t, err)
	require.NoError(t, backend.AssignProject(ctx, definition.ID, otherProject.ID))
	// A catalog outage must not prevent starting or controlling executions.
	isolated := &unavailableProjectCatalog{Backend: backend}
	s.modules = store.Bind(isolated)
	status := call("GET", "/api/v1/studio/runs/"+firstRunID, "")
	require.Equal(t, http.StatusOK, status.Code, status.Body.String())
	require.Contains(t, status.Body.String(), `"temporal_status":"running"`)
	canceled := call("POST", "/api/v1/studio/runs/"+firstRunID+"/cancel", "")
	require.Equal(t, http.StatusOK, canceled.Code, canceled.Body.String())
	require.Equal(t, firstRunID, c.cancelledID)
	for _, body := range []string{startBody, `{"definition_id":"project-independent","input":{}}`} {
		started = call("POST", "/api/v1/studio/runs", body)
		require.Equal(t, http.StatusCreated, started.Code, started.Body.String())
		require.Equal(t, published, c.input.Definition)
	}
	webhook := call("POST", "/api/v1/studio/webhooks/project-independent/start", `{}`)
	require.Equal(t, http.StatusCreated, webhook.Code, webhook.Body.String())
	require.Equal(t, published, c.input.Definition)
	require.Zero(t, isolated.reads, "execution must never query the project catalog")
}
