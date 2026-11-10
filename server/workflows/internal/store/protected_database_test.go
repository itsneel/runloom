package store

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/protection"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestProtectedDatabaseAndAtomicAudit(t *testing.T) {
	driver := os.Getenv("RUNLOOM_TEST_SERVICE_DATABASE")
	if driver == "" {
		t.Skip("local isolated service database settings required")
	}
	require.Contains(t, []string{"postgres", "mongodb"}, driver)
	directory := os.Getenv("RUNLOOM_TEST_SERVICE_CONFIG_DIR")
	t.Setenv("RUNLOOM_SECRETS_KEY_FILE", filepath.Join(directory, "workflow-key"))
	raw, err := os.ReadFile(filepath.Join(directory, "workflows-database-url"))
	require.NoError(t, err)
	conn, err := url.Parse(strings.TrimSpace(string(raw)))
	require.NoError(t, err)
	team := uuid.NewString()
	prefix := "runloom_team_" + strings.ReplaceAll(team, "-", "")
	ctx := context.Background()
	var b Backend
	if driver == "postgres" {
		conn.Host = "localhost:36432"
		pg, err := NewPostgresTeam(ctx, conn.String(), prefix)
		require.NoError(t, err)
		b = pg
		defer pg.Close(ctx)
		defer pg.db.Exec(`DROP SCHEMA "` + prefix + `" CASCADE`)
	} else {
		conn.Host = "localhost:37017"
		q := conn.Query()
		q.Set("directConnection", "true")
		conn.RawQuery = q.Encode()
		mg, err := NewMongoTeam(ctx, conn.String(), "runloom_workflows", prefix+"_")
		require.NoError(t, err)
		b = mg
		defer mg.Close(ctx)
		defer func() {
			names, err := mg.database.ListCollectionNames(ctx, bson.M{})
			require.NoError(t, err)
			for _, name := range names {
				if strings.HasPrefix(name, prefix+"_") {
					require.NoError(t, mg.database.Collection(name).Drop(ctx))
				}
			}
		}()
	}
	// Full authoring/run/project/lease contracts must still work with encryption.
	exerciseDatabaseStore(t, b)
	run := RunRecord{WorkflowID: "protected-run", DefinitionID: "test", Input: map[string]any{"private": "synthetic-private-input"}, TemporalStatus: "running"}
	_, err = b.RecordRunStart(run)
	require.NoError(t, err)
	_, err = b.UpdateRun(run.WorkflowID, "completed", &model.RunState{Status: "completed", Output: map[string]any{"private": "synthetic-private-output"}})
	require.NoError(t, err)
	saved, err := b.GetRun(run.WorkflowID)
	require.NoError(t, err)
	require.Equal(t, run.Input, saved.Input)
	events, err := b.ListAudit(run.WorkflowID, "", 100)
	require.NoError(t, err)
	require.Len(t, events, 2)
	event, err := b.AppendAudit(AuditEvent{WorkflowID: run.WorkflowID, Actor: "operator", EventType: "sensitive.read", Payload: map[string]any{"private": "synthetic-private-audit"}})
	require.NoError(t, err)
	switch db := b.(type) {
	case *PostgresStore:
		var raw string
		require.NoError(t, db.db.QueryRow(`SELECT input::text FROM runloom_runs WHERE workflow_id=$1`, run.WorkflowID).Scan(&raw))
		require.NotContains(t, raw, "synthetic-private-input")
		require.Contains(t, raw, "_runloom_encrypted")
		_, err = db.db.Exec(`UPDATE runloom_audit_events SET actor='tampered' WHERE event_id=$1`, event.EventID)
		require.NoError(t, err)
		_, err = b.ListAudit(run.WorkflowID, "", 100)
		require.ErrorIs(t, err, protection.ErrProtectedData)
		// Failure to write the audit must roll back the run insert itself.
		_, err = db.db.Exec(`ALTER TABLE runloom_audit_events ADD CONSTRAINT reject_test_audit CHECK (workflow_id <> 'blocked-run')`)
		require.NoError(t, err)
	case *MongoStore:
		var doc bson.M
		require.NoError(t, db.runs.FindOne(ctx, bson.M{"workflow_id": run.WorkflowID}).Decode(&doc))
		require.NotEqual(t, run.Input, doc["input"])
		_, err = db.audits.UpdateOne(ctx, bson.M{"event_id": event.EventID}, bson.M{"$set": bson.M{"actor": "tampered"}})
		require.NoError(t, err)
		_, err = b.ListAudit(run.WorkflowID, "", 100)
		require.ErrorIs(t, err, protection.ErrProtectedData)
		// The application role deliberately cannot collMod. Use a new disposable
		// validation-failing sink; do not grant administrative database privileges.
		name := prefix + "_rejected_audits"
		require.NoError(t, db.database.CreateCollection(ctx, name, options.CreateCollection().SetValidator(bson.M{"workflow_id": bson.M{"$ne": "blocked-run"}})))
		db.audits = db.database.Collection(name)
	}
	_, err = b.RecordRunStart(RunRecord{WorkflowID: "blocked-run", Input: map[string]any{}, TemporalStatus: "running"})
	require.Error(t, err)
	_, err = b.GetRun("blocked-run")
	require.ErrorIs(t, err, ErrNotFound)
}
