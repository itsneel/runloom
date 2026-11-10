package tests

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/example/runloom/workflows/internal/engine"
	"github.com/example/runloom/workflows/internal/protection"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	temporalclient "go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Exercises the deployed HTTP boundary, real worker, raw database and raw
// Temporal history. Only synthetic data and loopback installations are allowed.
func TestSecurityStageOneLive(t *testing.T) {
	driver := os.Getenv("RUNLOOM_TEST_SERVICE_DATABASE")
	if driver == "" {
		t.Skip("local security integration settings required")
	}
	require.Contains(t, []string{"mongodb", "postgres"}, driver)
	base := os.Getenv("RUNLOOM_UI_URL")
	u, err := url.Parse(base)
	require.NoError(t, err)
	require.Contains(t, []string{"localhost", "127.0.0.1"}, u.Hostname())
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	team := os.Getenv("RUNLOOM_TEST_TEAM_ID")
	_, err = uuid.Parse(team)
	require.NoError(t, err)
	token := os.Getenv("RUNLOOM_TEST_AUTHORIZATION")
	require.NotEmpty(t, token)
	call := func(method, path string, body any) map[string]any {
		t.Helper()
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(raw))
		require.NoError(t, err)
		req.Header.Set("Authorization", token)
		req.Header.Set("X-Runloom-Team", team)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Less(t, resp.StatusCode, 300, "HTTP failure on %s", path)
		require.NotEmpty(t, resp.Header.Get("X-Runloom-Request-ID"), "missing durable request receipt")
		var out map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		return out
	}
	marker := "synthetic-health-data-" + uuid.NewString()
	id := "security-" + uuid.NewString()
	call("POST", "/api/v1/studio/definitions", map[string]any{"schema_version": "1.0", "id": id, "name": "Security verification", "trigger": map[string]any{"type": "manual"}, "steps": []any{map[string]any{"id": "echo", "type": "activity", "activity": "runloom.echo", "with": map[string]any{"value": "${input.value}"}}, map[string]any{"id": "pause", "type": "pause", "message": "Security checkpoint"}}, "output": "${steps.echo.output}"})
	call("POST", "/api/v1/studio/definitions/"+id+"/publish", map[string]any{})
	started := call("POST", "/api/v1/studio/runs", map[string]any{"definition_id": id, "input": map[string]any{"value": marker}})
	run := started["workflow_id"].(string)
	require.Eventually(t, func() bool {
		v := call("GET", "/api/v1/studio/runs/"+run, nil)
		s, _ := v["state"].(map[string]any)
		return s["status"] == "paused"
	}, 25*time.Second, 250*time.Millisecond)
	call("POST", "/api/v1/studio/runs/"+run+"/resume", map[string]any{"step_id": "pause", "payload": map[string]any{"private": marker}})
	require.Eventually(t, func() bool { return call("GET", "/api/v1/studio/runs/"+run, nil)["temporal_status"] == "completed" }, 25*time.Second, 250*time.Millisecond)
	history := call("GET", "/api/v1/studio/runs/"+run+"/history", nil)
	require.Equal(t, marker, history["run"].(map[string]any)["output"].(map[string]any)["value"])

	directory := os.Getenv("RUNLOOM_TEST_SERVICE_CONFIG_DIR")
	t.Setenv("RUNLOOM_SECRETS_KEY_FILE", filepath.Join(directory, "workflow-key"))
	metadata := history["run"].(map[string]any)["metadata"].(map[string]any)
	require.Equal(t, os.Getenv("RUNLOOM_TEST_ORG_ID"), metadata["org_id"])
	cfg, err := protection.TemporalOptions(os.Getenv("RUNLOOM_TEST_TEMPORAL_ADDRESS"), metadata["namespace"].(string))
	require.NoError(t, err)
	c, err := temporalclient.Dial(cfg)
	require.NoError(t, err)
	defer c.Close()
	events := &historypb.History{}
	it := c.GetWorkflowHistory(ctx, run, "", false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	payloads := 0
	for it.HasNext() {
		e, err := it.Next()
		require.NoError(t, err)
		events.Events = append(events.Events, e)
		payloads += checkEncryptedPayloads(t, e.ProtoReflect(), marker)
	}
	require.Greater(t, payloads, 4)
	replay, err := worker.NewWorkflowReplayerWithOptions(worker.WorkflowReplayerOptions{DataConverter: cfg.DataConverter, FailureConverter: cfg.FailureConverter})
	require.NoError(t, err)
	replay.RegisterWorkflow(engine.RunloomWorkflow)
	require.NoError(t, replay.ReplayWorkflowHistory(protection.SafeLogger{}, events))

	// Schedule memo and start inputs use the same encrypted converter as runs.
	schedule := call("POST", "/api/v1/studio/schedules", map[string]any{"name": "Protected schedule", "definition_id": id, "version": 1, "input": map[string]any{"value": marker}, "interval_seconds": 3600, "paused": true})
	scheduleID := schedule["id"].(string)
	described := call("GET", "/api/v1/studio/schedules/"+scheduleID, nil)
	require.Equal(t, marker, described["input"].(map[string]any)["value"])
	call("DELETE", "/api/v1/studio/schedules/"+scheduleID, nil)

	// Errors thrown by custom code must not leak input via failure messages or
	// stack traces in raw Temporal history. The API can still show authorized users.
	failedID := "security-error-" + uuid.NewString()
	call("POST", "/api/v1/studio/definitions", map[string]any{"schema_version": "1.0", "id": failedID, "name": "Protected failure", "trigger": map[string]any{"type": "manual"}, "steps": []any{map[string]any{"id": "fail", "type": "code", "code": map[string]any{"runtime": "quickjs-ng-0.11.0-v1", "source": "throw new Error(input.value);"}, "with": map[string]any{"value": "${input.value}"}}}})
	call("POST", "/api/v1/studio/definitions/"+failedID+"/publish", map[string]any{})
	failedStart := call("POST", "/api/v1/studio/runs", map[string]any{"definition_id": failedID, "input": map[string]any{"value": marker}})
	failedRun := failedStart["workflow_id"].(string)
	require.Eventually(t, func() bool { return call("GET", "/api/v1/studio/runs/"+failedRun, nil)["temporal_status"] == "failed" }, 25*time.Second, 250*time.Millisecond)
	failures := c.GetWorkflowHistory(ctx, failedRun, "", false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	for failures.HasNext() {
		event, err := failures.Next()
		require.NoError(t, err)
		checkEncryptedPayloads(t, event.ProtoReflect(), marker)
	}

	credentials, err := os.ReadFile(filepath.Join(directory, "workflows-database-url"))
	require.NoError(t, err)
	conn, err := url.Parse(strings.TrimSpace(string(credentials)))
	require.NoError(t, err)
	var stored []byte
	prefix := "runloom_team_" + strings.ReplaceAll(team, "-", "")
	if driver == "postgres" {
		conn.Host = "localhost:36432"
		db, err := sql.Open("pgx", conn.String())
		require.NoError(t, err)
		defer db.Close()
		err = db.QueryRowContext(ctx, fmt.Sprintf(`SELECT json_build_object('input',input,'state',state,'output',output)::text FROM "%s".runloom_runs WHERE workflow_id=$1`, prefix), run).Scan(&stored)
		require.NoError(t, err)
	} else {
		conn.Host = "localhost:37017"
		query := conn.Query()
		query.Set("directConnection", "true")
		conn.RawQuery = query.Encode()
		db, err := mongo.Connect(options.Client().ApplyURI(conn.String()))
		require.NoError(t, err)
		defer db.Disconnect(context.Background())
		var record bson.M
		require.NoError(t, db.Database("runloom_workflows").Collection(prefix+"_workflow_runs").FindOne(ctx, bson.M{"workflow_id": run}).Decode(&record))
		stored, err = json.Marshal(record)
		require.NoError(t, err)
	}
	require.False(t, bytes.Contains(stored, []byte(marker)), "plaintext execution data in database")
	require.Contains(t, string(stored), "_runloom_encrypted")
	// A caller cannot replace authenticated identity or select another team.
	req, _ := http.NewRequestWithContext(ctx, "GET", base+"/api/v1/studio/runs/"+run, nil)
	req.Header.Set("Authorization", token)
	req.Header.Set("X-Runloom-Team", uuid.NewString())
	req.Header.Set("X-Runloom-Actor", "system")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	require.Equal(t, 403, resp.StatusCode)
}

func checkEncryptedPayloads(t *testing.T, m protoreflect.Message, marker string) int {
	t.Helper()
	if a, ok := m.Interface().(*commonpb.SearchAttributes); ok {
		for _, p := range a.IndexedFields {
			require.False(t, bytes.Contains(p.Data, []byte(marker)), "sensitive data in plaintext search attributes")
		}
		return 0
	}
	if p, ok := m.Interface().(*commonpb.Payload); ok {
		require.Equal(t, "binary/runloom-encrypted-v1", string(p.Metadata["encoding"]))
		require.False(t, bytes.Contains(p.Data, []byte(marker)), "plaintext in Temporal payload")
		return 1
	}
	count := 0
	m.Range(func(f protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if !f.IsList() && !f.IsMap() && f.Kind() == protoreflect.StringKind {
			require.NotContains(t, v.String(), marker, "sensitive error text in Temporal metadata")
		}
		if f.IsMap() {
			if f.MapValue().Kind() == protoreflect.MessageKind {
				v.Map().Range(func(_ protoreflect.MapKey, v protoreflect.Value) bool {
					count += checkEncryptedPayloads(t, v.Message(), marker)
					return true
				})
			}
		} else if f.IsList() {
			if f.Kind() == protoreflect.MessageKind {
				for i := 0; i < v.List().Len(); i++ {
					count += checkEncryptedPayloads(t, v.List().Get(i).Message(), marker)
				}
			}
		} else if f.Kind() == protoreflect.MessageKind {
			count += checkEncryptedPayloads(t, v.Message(), marker)
		}
		return true
	})
	return count
}
