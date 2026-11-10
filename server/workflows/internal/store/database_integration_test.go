package store

import (
	"context"
	"os"
	"testing"

	"github.com/example/runloom/workflows/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func exerciseDatabaseStore(t *testing.T, values Backend) {
	t.Helper()
	exerciseInvalidDrafts(t, values)
	exerciseProjects(t, values)
	exerciseEditLocks(t, values)
	suffix := uuid.NewString()[:12]
	definitionID := "dbtest-" + suffix
	workflowID := "run-" + suffix
	value := model.Definition{
		SchemaVersion: "1.0",
		ID:            definitionID,
		Name:          "Database contract test",
		Description:   "Exercises the persistent store contract",
		Trigger:       model.Trigger{Type: "manual"},
		InputSchema:   map[string]any{"type": "object"},
		Steps:         []model.Step{{ID: "echo", Type: "activity", Activity: "runloom.echo"}},
		Output:        "${steps.echo.output}",
		Layout:        &model.CanvasLayout{Positions: map[string]model.CanvasPoint{"echo": {X: -200, Y: 320}}},
	}
	_, err := values.SaveDraft(value)
	require.NoError(t, err)
	published, err := values.Publish(definitionID)
	require.NoError(t, err)
	require.Equal(t, 1, published.Version)
	storedDefinition, err := values.Get(definitionID, nil)
	require.NoError(t, err)
	require.Equal(t, value.Name, storedDefinition.Name)
	require.Equal(t, value.Layout, storedDefinition.Layout)
	commits, err := values.ListCommits(definitionID, 10)
	require.NoError(t, err)
	require.Equal(t, []string{"publish", "save"}, []string{commits[0].Kind, commits[1].Kind})
	source, err := values.GetCommit(definitionID, commits[1].CommitID)
	require.NoError(t, err)
	require.NotNil(t, source.Body)
	require.Equal(t, definitionID, source.Body.ID)
	require.Equal(t, value.Layout, source.Body.Layout)
	restored, restoreCommit, err := values.RestoreCommit(definitionID, source.CommitID, CommitMeta{Actor: "database-test", Message: "Restore tested snapshot"})
	require.NoError(t, err)
	require.Equal(t, 0, restored.Version)
	require.Equal(t, value.Layout, restored.Layout)
	require.Equal(t, "restore", restoreCommit.Kind)
	require.Equal(t, int64(3), restoreCommit.Sequence)

	_, err = values.RecordRunStart(RunRecord{WorkflowID: workflowID, TemporalRunID: "temporal-" + suffix, DefinitionID: definitionID, DefinitionVersion: 1, TriggerType: "manual", Input: map[string]any{"value": "forty-two"}, TemporalStatus: "running"})
	require.NoError(t, err)
	_, err = values.AppendAudit(AuditEvent{WorkflowID: workflowID, DefinitionID: definitionID, EventType: "workflow.started", Actor: "test"})
	require.NoError(t, err)
	changed, err := values.UpdateRun(workflowID, "completed", &model.RunState{Status: "completed", Output: map[string]any{"value": "forty-two"}})
	require.NoError(t, err)
	require.True(t, changed)
	storedRun, err := values.GetRun(workflowID)
	require.NoError(t, err)
	require.Equal(t, "forty-two", storedRun.Input["value"])
	require.Equal(t, "forty-two", storedRun.Output.(map[string]any)["value"])
	events, err := values.ListAudit(workflowID, "", 10)
	require.NoError(t, err)
	found := false
	for _, event := range events {
		if event.EventType == "workflow.started" && event.Actor == "test" {
			found = true
		}
	}
	require.True(t, found)
}

func TestPostgresStoreContract(t *testing.T) {
	url := os.Getenv("RUNLOOM_TEST_POSTGRES_URL")
	if url == "" {
		t.Skip("RUNLOOM_TEST_POSTGRES_URL is not configured")
	}
	values, err := NewPostgres(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, values.Close(context.Background())) })
	exerciseDatabaseStore(t, values)
	exercisePromptStore(t, values)
}

func TestMongoPublishRollsBackWhenCommitFails(t *testing.T) {
	url := os.Getenv("RUNLOOM_TEST_MONGODB_URL")
	if url == "" {
		t.Skip("opt-in MongoDB")
	}
	ctx := context.Background()
	b, err := NewMongo(ctx, url, "runloom_atomic_test_"+uuid.NewString())
	require.NoError(t, err)
	defer b.Close(ctx)
	defer b.database.Drop(ctx)
	_, err = b.SaveDraft(model.Definition{SchemaVersion: "1.0", ID: "atomic", Name: "Atomic", Steps: []model.Step{{ID: "done", Type: "end"}}})
	require.NoError(t, err)
	err = b.database.RunCommand(ctx, bson.D{{Key: "collMod", Value: b.commits.Name()}, {Key: "validator", Value: bson.M{"kind": bson.M{"$ne": "publish"}}}}).Err()
	require.NoError(t, err)
	_, err = b.Publish("atomic")
	require.Error(t, err)
	_, err = b.Get("atomic", nil)
	require.ErrorIs(t, err, ErrNotFound)
	commits, err := b.ListCommits("atomic", 10)
	require.NoError(t, err)
	require.Len(t, commits, 1)
}

func TestMongoStoreContract(t *testing.T) {
	url := os.Getenv("RUNLOOM_TEST_MONGODB_URL")
	if url == "" {
		t.Skip("RUNLOOM_TEST_MONGODB_URL is not configured")
	}
	databaseName := "runloom_test_" + uuid.NewString()
	values, err := NewMongo(context.Background(), url, databaseName)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, values.Close(context.Background())) })
	t.Cleanup(func() { require.NoError(t, values.database.Drop(context.Background())) })
	exerciseDatabaseStore(t, values)
	exercisePromptStore(t, values)
}
