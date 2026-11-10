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
	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/services"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAuthoringPolicy(t *testing.T) {
	root := model.Definition{SchemaVersion: "1.0", ID: "parent", Name: "Parent", Kind: "main", Steps: []model.Step{{ID: "done", Type: "end"}}, Description: "DO NOT DISCLOSE"}
	graph, err := authoring.Graph(root, "project", func(string, *int) (model.Definition, error) { return model.Definition{}, store.ErrNotFound })
	require.NoError(t, err)
	raw, _ := json.Marshal(graph)
	require.NotContains(t, string(raw), "DO NOT DISCLOSE")
	proposal := authoring.Proposal{Name: "Child", Steps: []model.Step{{ID: "wait", Type: "wait", Seconds: 1}, {ID: "done", Type: "end"}}}
	parent, child, err := authoring.Build(root, graph, "child-"+uuid.NewString(), proposal)
	require.NoError(t, err)
	require.Equal(t, "child", child.Kind)
	require.Zero(t, child.Version)
	require.Equal(t, "subworkflow", parent.Steps[0].Type)
	require.Equal(t, "done", parent.Steps[1].ID)
	_, err = definition.Normalize(parent)
	require.Error(t, err, "draft child references cannot publish or execute")
	for _, bad := range []authoring.Proposal{
		{Name: "Bad", Steps: []model.Step{{ID: "call", Type: "subworkflow", DefinitionID: "unrelated", Version: 1}}},
		{Name: "Bad", Steps: []model.Step{{ID: "call", Type: "subworkflow", DefinitionID: root.ID, Version: 0}}},
		{Name: "Bad", Steps: []model.Step{{ID: "bad", Type: "invented"}}},
		{Name: "Bad", Steps: proposal.Steps, BeforeStepID: "missing"},
	} {
		_, _, err = authoring.Build(root, graph, "child-"+uuid.NewString(), bad)
		require.Error(t, err)
	}
	root.Steps = []model.Step{{ID: "cycle", Type: "subworkflow", DefinitionID: "parent", Version: 0}}
	_, err = authoring.Graph(root, "project", func(string, *int) (model.Definition, error) { d := root; d.Kind = "child"; return d, nil })
	require.ErrorContains(t, err, "circular")
}

func TestLiveAuthoringAtomicAPI(t *testing.T) {
	ctx := context.Background()
	var backend store.Backend
	var err error
	if uri := os.Getenv("RUNLOOM_TEST_POSTGRES_URL"); uri != "" {
		backend, err = store.NewPostgres(ctx, uri)
	} else if uri := os.Getenv("RUNLOOM_TEST_MONGODB_URL"); uri != "" {
		backend, err = store.NewMongo(ctx, uri, "runloom_authoring_test_"+strings.ReplaceAll(uuid.NewString(), "-", ""))
	} else {
		t.Skip("disposable database URL required")
	}
	require.NoError(t, err)
	defer backend.Close(ctx)
	root, err := backend.SaveDraft(model.Definition{SchemaVersion: "1.0", ID: "parent-" + uuid.NewString(), Name: "Parent", Kind: "main", Steps: []model.Step{{ID: "done", Type: "end"}}})
	require.NoError(t, err)
	project, err := backend.CreateProject(ctx, "Authoring "+uuid.NewString())
	require.NoError(t, err)
	require.NoError(t, backend.AssignProject(ctx, root.ID, project.ID))
	modules := store.Bind(backend)
	modules.Catalog = &services.Backend{Backend: backend, Role: "workflows"}
	server := api.NewModules(modules, nil, "test")
	defer server.Close()
	call := func(method string, body any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "/api/v1/studio/definitions/"+root.ID+"/authoring", bytes.NewReader(b))
		r.Header.Set("X-Runloom-Actor", "test-editor")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, r)
		return w
	}
	get := call("GET", nil)
	require.Equal(t, 200, get.Code, get.Body.String())
	var graph authoring.Context
	require.NoError(t, json.Unmarshal(get.Body.Bytes(), &graph))
	id := uuid.NewString()
	body := map[string]any{"request_id": id, "revision": graph.Revision, "approved": true, "proposal": authoring.Proposal{Name: "Reviewed child", Steps: []model.Step{{ID: "done", Type: "end"}}}}
	body["approved"] = false
	require.Equal(t, 400, call("POST", body).Code)
	body["approved"] = true
	token := uuid.NewString()
	_, err = backend.ChangeEditLock(ctx, root.ID, "another-editor", "Other", token, "acquire")
	require.NoError(t, err)
	require.Equal(t, 423, call("POST", body).Code)
	_, err = backend.ChangeEditLock(ctx, root.ID, "another-editor", "Other", token, "release")
	require.NoError(t, err)
	response := call("POST", body)
	require.Equal(t, 200, response.Code, response.Body.String())
	zero := 0
	parent, err := backend.Get(root.ID, &zero)
	require.NoError(t, err)
	require.Len(t, parent.Steps, 2)
	require.Equal(t, "child-"+id, parent.Steps[0].DefinitionID)
	child, err := backend.Get("child-"+id, &zero)
	require.NoError(t, err)
	require.Equal(t, "child", child.Kind)
	_, err = backend.Get(child.ID, nil)
	require.ErrorIs(t, err, store.ErrNotFound)
	catalog, err := backend.ProjectCatalog(ctx)
	require.NoError(t, err)
	require.Equal(t, project.ID, catalog.WorkflowProject(child.ID))
	require.Equal(t, 200, call("POST", body).Code, "lost acknowledgement replay must succeed without another child")
	commits, err := backend.ListCommits(child.ID, 10)
	require.NoError(t, err)
	require.Len(t, commits, 1)
	body["request_id"] = uuid.NewString()
	require.Equal(t, 409, call("POST", body).Code, "old graph revision is stale")
	body["request_id"] = id
	body["proposal"] = authoring.Proposal{Name: "Changed", Steps: []model.Step{{ID: "done", Type: "end"}}}
	require.Equal(t, 409, call("POST", body).Code, "idempotency key cannot change body")
	events, err := backend.ListAudit("", root.ID, 30)
	require.NoError(t, err)
	found := false
	for _, event := range events {
		if event.EventType == "definition.assistant_child_attached" {
			found = true
			require.Equal(t, "test-editor", event.Actor)
		}
	}
	require.True(t, found)
	// Storage-level stale revisions also fail without leaving a child.
	atomic := backend.(store.Authoring)
	badID := uuid.NewString()
	badChild := child
	badChild.ID = "child-" + badID
	_, err = atomic.CommitAuthoring(ctx, store.AuthoringBatch{RequestID: badID, Fingerprint: "bad", Actor: "test", ParentHash: "stale", ProjectID: project.ID, Parent: parent, Child: badChild})
	require.ErrorIs(t, err, store.ErrAuthoringConflict)
	_, err = backend.Get(badChild.ID, &zero)
	require.ErrorIs(t, err, store.ErrNotFound)
}
