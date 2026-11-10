package workflows_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/example/runloom/workflows/internal/authoring"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Force the parent's commit insert to fail AFTER the child insert. The child,
// parent edit, project assignment, audit and receipt must all roll back.
func TestLiveAuthoringRollsBack(t *testing.T) {
	ctx := context.Background()
	var backend store.Backend
	var err error
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	parentID := "parent-" + suffix
	if uri := os.Getenv("RUNLOOM_TEST_POSTGRES_URL"); uri != "" {
		backend, err = store.NewPostgres(ctx, uri)
		require.NoError(t, err)
		db, e := sql.Open("pgx", uri)
		require.NoError(t, e)
		defer db.Close()
		constraint := "authoring_failure_" + suffix
		_, err = db.Exec(`ALTER TABLE runloom_definition_commits ADD CONSTRAINT ` + constraint + ` CHECK (actor <> 'fail-authoring')`)
		require.NoError(t, err)
		defer db.Exec(`ALTER TABLE runloom_definition_commits DROP CONSTRAINT ` + constraint)
	} else if uri := os.Getenv("RUNLOOM_TEST_MONGODB_URL"); uri != "" {
		name := "runloom_atomic_authoring_" + suffix
		backend, err = store.NewMongo(ctx, uri, name)
		require.NoError(t, err)
		client, e := mongo.Connect(options.Client().ApplyURI(uri))
		require.NoError(t, e)
		defer client.Disconnect(ctx)
		defer client.Database(name).Drop(ctx)
		err = client.Database(name).RunCommand(ctx, bson.D{{Key: "collMod", Value: "definition_commits"}, {Key: "validator", Value: bson.M{"$nor": []bson.M{{"actor": "fail-authoring"}}}}}).Err()
		require.NoError(t, err)
	} else {
		t.Skip("disposable database required")
	}
	defer backend.Close(ctx)
	parent, err := backend.SaveDraft(model.Definition{SchemaVersion: "1.0", ID: parentID, Name: "Parent", Kind: "main", Steps: []model.Step{{ID: "done", Type: "end"}}})
	require.NoError(t, err)
	graph, err := authoring.Graph(parent, "default", backend.Get)
	require.NoError(t, err)
	id := uuid.NewString()
	changed, child, err := authoring.Build(parent, graph, "child-"+id, authoring.Proposal{Name: "Child", Steps: []model.Step{{ID: "done", Type: "end"}}})
	require.NoError(t, err)
	atomic := backend.(store.Authoring)
	_, err = atomic.CommitAuthoring(ctx, store.AuthoringBatch{RequestID: id, Fingerprint: "rollback", Actor: "fail-authoring", ParentHash: authoring.Fingerprint(parent), ProjectID: "default", Parent: changed, Child: child})
	require.Error(t, err)
	zero := 0
	_, err = backend.Get(child.ID, &zero)
	require.ErrorIs(t, err, store.ErrNotFound)
	actual, err := backend.Get(parent.ID, &zero)
	require.NoError(t, err)
	require.Equal(t, authoring.Fingerprint(parent), authoring.Fingerprint(actual))
	_, err = atomic.AuthoringReceipt(ctx, id)
	require.ErrorIs(t, err, store.ErrNotFound)
	commits, err := backend.ListCommits(child.ID, 10)
	require.NoError(t, err)
	require.Empty(t, commits)
	projects, err := backend.ProjectCatalog(ctx)
	require.NoError(t, err)
	require.NotContains(t, projects.WorkflowProjects, child.ID)
	events, err := backend.ListAudit("", parent.ID, 20)
	require.NoError(t, err)
	for _, event := range events {
		require.NotEqual(t, "definition.assistant_child_attached", event.EventType)
	}
	// The same transaction guarantee applies to single-root creation and updates.
	for _, mode := range []string{"create", "update"} {
		requestID := uuid.NewString()
		candidate := parent
		candidate.Name = "Must roll back"
		if mode == "create" {
			candidate.ID = "workflow-" + requestID
		}
		_, err = atomic.CommitAuthoring(ctx, store.AuthoringBatch{RequestID: requestID, Fingerprint: "rollback-" + mode, Actor: "fail-authoring", ParentHash: authoring.Fingerprint(parent), ProjectID: "default", Parent: candidate, Mode: mode})
		require.Error(t, err)
		_, err = atomic.AuthoringReceipt(ctx, requestID)
		require.ErrorIs(t, err, store.ErrNotFound)
		actual, err = backend.Get(candidate.ID, &zero)
		if mode == "create" {
			require.ErrorIs(t, err, store.ErrNotFound)
		} else {
			require.NoError(t, err)
			require.Equal(t, parent.Name, actual.Name)
		}
	}

}
