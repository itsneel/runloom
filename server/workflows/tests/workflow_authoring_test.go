package workflows_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/example/runloom/workflows/internal/api"
	"github.com/example/runloom/workflows/internal/authoring"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestWorkflowAuthoringPolicy(t *testing.T) {
	root := model.Definition{SchemaVersion: "1.0", ID: "parent", Name: "Original", Kind: "main", Steps: []model.Step{{ID: "wait", Type: "wait", Seconds: 1}}}
	updated := root
	updated.Name = "Enhanced"
	updated.Steps = []model.Step{{ID: "wait", Type: "wait", Seconds: 5}, {ID: "done", Type: "end"}}
	result, err := authoring.BuildWorkflow(root, authoring.Context{RootID: "parent"}, "parent", authoring.Proposal{Operation: "update", Definition: &updated})
	require.NoError(t, err)
	require.Equal(t, 5, result.Steps[0].Seconds)
	require.Zero(t, result.Version)
	created, err := authoring.BuildWorkflow(model.Definition{}, authoring.Context{}, "workflow-new", authoring.Proposal{Operation: "create", Definition: &updated})
	require.NoError(t, err)
	require.Equal(t, "workflow-new", created.ID)
	require.Equal(t, "main", created.Kind)
	updated.ID = "unrelated"
	_, err = authoring.BuildWorkflow(root, authoring.Context{RootID: "parent"}, "parent", authoring.Proposal{Operation: "update", Definition: &updated})
	require.Error(t, err)
	updated.ID = root.ID
	updated.Steps = []model.Step{{ID: "bad", Type: "invented"}}
	_, err = authoring.BuildWorkflow(root, authoring.Context{RootID: "parent"}, "parent", authoring.Proposal{Operation: "update", Definition: &updated})
	require.Error(t, err)
	updated.Steps = []model.Step{{ID: "call", Type: "subworkflow", DefinitionID: "unrelated", Version: 1}}
	_, err = authoring.BuildWorkflow(root, authoring.Context{RootID: "parent"}, "parent", authoring.Proposal{Operation: "update", Definition: &updated})
	require.Error(t, err)
}

func TestLiveWorkflowCreateAndUpdate(t *testing.T) {
	ctx := context.Background()
	var backend store.Backend
	var err error
	if uri := os.Getenv("RUNLOOM_TEST_POSTGRES_URL"); uri != "" {
		backend, err = store.NewPostgres(ctx, uri)
	} else if uri := os.Getenv("RUNLOOM_TEST_MONGODB_URL"); uri != "" {
		backend, err = store.NewMongo(ctx, uri, "lumi_test_"+strings.ReplaceAll(uuid.NewString(), "-", ""))
	} else {
		t.Skip("disposable database required")
	}
	require.NoError(t, err)
	defer backend.Close(ctx)
	server := api.New(backend, nil, "test")
	defer server.Close()
	call := func(method, path string, body any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("X-Runloom-Actor", "verified-test-editor")
		if method == "GET" {
			r.Header.Set("X-Runloom-Authoring-Context", "full")
		}
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, r)
		return w
	}
	path := "/api/v1/studio/authoring/workflows"
	get := call("GET", path, nil)
	require.Equal(t, 200, get.Code, get.Body.String())
	var scope authoring.Context
	require.NoError(t, json.Unmarshal(get.Body.Bytes(), &scope))
	id := uuid.NewString()
	d := model.Definition{SchemaVersion: "1.0", ID: "model-placeholder", Name: "Created by Lumi", Kind: "main", Steps: []model.Step{{ID: "wait", Type: "wait", Seconds: 2}}}
	request := map[string]any{"request_id": id, "revision": scope.Revision, "approved": true, "proposal": authoring.Proposal{Operation: "create", Definition: &d}}
	first := call("POST", path, request)
	require.Equal(t, 200, first.Code, first.Body.String())
	repeat := call("POST", path, request)
	require.Equal(t, 200, repeat.Code, repeat.Body.String())
	require.JSONEq(t, first.Body.String(), repeat.Body.String())
	zero := 0
	root, err := backend.Get("workflow-"+id, &zero)
	require.NoError(t, err)
	require.Equal(t, d.Name, root.Name)
	require.Equal(t, "workflow-"+id, root.ID)
	projects, e := backend.ProjectCatalog(ctx)
	require.NoError(t, e)
	graphCheck, e := authoring.Graph(root, projects.WorkflowProject(root.ID), backend.Get)
	require.NoError(t, e)
	for _, ref := range graphCheck.Workflows {
		_, e = backend.Get(ref.ID, &ref.Version)
		require.NoError(t, e, "graph reference %s v%d", ref.ID, ref.Version)
	}
	published, e := backend.Publish(root.ID)
	require.NoError(t, e)
	scoped := "/api/v1/studio/definitions/" + root.ID + "/authoring"
	get = call("GET", scoped, nil)
	require.Equal(t, 200, get.Code, get.Body.String())
	require.NoError(t, json.Unmarshal(get.Body.Bytes(), &scope))
	require.NotNil(t, scope.Workflows[0].Definition)
	require.NotEmpty(t, scope.Integrations)
	originalRevision := scope.Revision
	root.Name = "Fixed workflow"
	root.Steps[0].Seconds = 8
	request = map[string]any{"request_id": uuid.NewString(), "revision": scope.Revision, "approved": true, "proposal": authoring.Proposal{Operation: "update", Definition: &root}}
	token := uuid.NewString()
	_, e = backend.ChangeEditLock(ctx, root.ID, "another-editor", "Another editor", token, "acquire")
	require.NoError(t, e)
	blocked := call("POST", scoped, request)
	require.Equal(t, 423, blocked.Code, blocked.Body.String())
	_, e = backend.ChangeEditLock(ctx, root.ID, "another-editor", "Another editor", token, "release")
	require.NoError(t, e)
	first = call("POST", scoped, request)
	require.Equal(t, 200, first.Code, first.Body.String())
	repeat = call("POST", scoped, request)
	require.Equal(t, 200, repeat.Code, repeat.Body.String())
	saved, err := backend.Get(root.ID, &zero)
	require.NoError(t, err)
	require.Equal(t, "Fixed workflow", saved.Name)
	request["request_id"] = uuid.NewString()
	request["revision"] = originalRevision
	stale := call("POST", scoped, request)
	require.Equal(t, 409, stale.Code, stale.Body.String())
	history, err := backend.ListCommits(root.ID, 100)
	require.NoError(t, err)
	require.Len(t, history, 3)
	immutable, e := backend.Get(root.ID, &published.Version)
	require.NoError(t, e)
	require.Equal(t, "Created by Lumi", immutable.Name)
}

func TestWorkflowNamedAuthoringRemainsReadable(t *testing.T) {
	backend, err := store.New(t.TempDir())
	require.NoError(t, err)
	_, err = backend.SaveDraft(model.Definition{SchemaVersion: "1.0", ID: "authoring", Name: "Existing authoring workflow", Steps: []model.Step{{ID: "done", Type: "end"}}})
	require.NoError(t, err)
	server := api.New(backend, nil, "test")
	defer server.Close()
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/studio/definitions/authoring?version=0", nil))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "Existing authoring workflow")
}
