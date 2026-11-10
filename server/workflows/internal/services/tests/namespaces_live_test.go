package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/example/runloom/workflows/internal/engine"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/protection"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	enums "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/api/workflowservice/v1"
	temporalclient "go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/worker"
)

// Runs unchanged against Compose or a port-forwarded local Helm installation.
// Creates synthetic orgs after startup: no namespace CLI or service restart.
func TestOrganizationNamespacesLive(t *testing.T) {
	base, address := os.Getenv("RUNLOOM_UI_URL"), os.Getenv("RUNLOOM_TEST_TEMPORAL_ADDRESS")
	if os.Getenv("RUNLOOM_TEST_AUTHORIZATION") == "" || address == "" {
		t.Skip("local authenticated deployment required")
	}
	u, err := url.Parse(base)
	require.NoError(t, err)
	require.Contains(t, []string{"localhost", "127.0.0.1"}, u.Hostname())
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	t.Setenv("RUNLOOM_SECRETS_KEY_FILE", filepath.Join(os.Getenv("RUNLOOM_TEST_SERVICE_CONFIG_DIR"), "workflow-key"))
	team, project := "", ""
	call := func(method, path string, body, out any) int {
		t.Helper()
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		req, err := http.NewRequestWithContext(ctx, method, base+"/api/v1/studio"+path, bytes.NewReader(raw))
		require.NoError(t, err)
		req.Header.Set("Authorization", os.Getenv("RUNLOOM_TEST_AUTHORIZATION"))
		req.Header.Set("X-Runloom-Team", team)
		if project != "" {
			req.Header.Set("X-Runloom-Project", project)
		}
		req.Header.Set("X-Runloom-Org", "forged-org")
		req.Header.Set("X-Runloom-Namespace", "default")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		if resp.StatusCode == 404 {
			_, _ = io.Copy(io.Discard, resp.Body)
			return resp.StatusCode
		}
		if out != nil {
			require.Less(t, resp.StatusCode, 300, "%s %s failed", method, path)
			require.NoError(t, json.NewDecoder(resp.Body).Decode(out))
		} else {
			_, _ = io.Copy(io.Discard, resp.Body)
		}
		return resp.StatusCode
	}
	save := func(d model.Definition) {
		var out any
		call("POST", "/definitions", d, &out)
		call("POST", "/definitions/"+d.ID+"/publish", map[string]any{}, &out)
	}
	wait := func(id string) *model.RunState {
		var state *model.RunState
		require.Eventually(t, func() bool {
			var result struct {
				State *model.RunState `json:"state"`
			}
			if call("GET", "/runs/"+id, nil, &result) != 200 {
				return false
			}
			state = result.State
			return state != nil && state.Status == "completed"
		}, 40*time.Second, 300*time.Millisecond)
		return state
	}
	history := func(id string) store.RunRecord {
		var h struct {
			Run store.RunRecord `json:"run"`
		}
		call("GET", "/runs/"+id+"/history", nil, &h)
		return h.Run
	}
	var firstID, firstNamespace string
	for n := 0; n < 2; n++ {
		var org, created map[string]string
		call("POST", "/orgs", map[string]string{"name": "Namespace verification " + uuid.NewString()}, &org)
		call("POST", "/orgs/"+org["id"]+"/teams", map[string]string{"name": "Execution tests"}, &created)
		team = created["id"]
		var folder map[string]string
		call("POST", "/projects", map[string]string{"name": "Child project"}, &folder)
		project = folder["id"]
		child := model.Definition{SchemaVersion: "1.0", ID: "namespace-child", Name: "Child", Kind: "child", Trigger: model.Trigger{Type: "manual"}, Steps: []model.Step{{ID: "echo", Type: "activity", Activity: "runloom.echo", With: map[string]any{"value": "synthetic"}}}, Output: "${steps.echo.output}"}
		save(child)
		var published any
		call("POST", "/definitions/"+child.ID+"/publish", map[string]any{}, &published)
		project = "default"
		parent := model.Definition{SchemaVersion: "1.0", ID: "namespace-parent", Name: "Parent", Kind: "main", Trigger: model.Trigger{Type: "manual"}, Steps: []model.Step{{ID: "child", Type: "subworkflow", DefinitionID: child.ID, Version: 2}}, Output: "${steps.child.output}"}
		save(parent)
		var start struct {
			WorkflowID string `json:"workflow_id"`
		}
		// Both orgs intentionally reuse definition IDs and requested execution ID.
		call("POST", "/runs", map[string]any{"definition_id": parent.ID, "workflow_id": "isolation-check"}, &start)
		id := start.WorkflowID
		state := wait(id)
		record := history(id)
		m := record.Metadata
		require.NotNil(t, m)
		require.Equal(t, org["id"], m.OrgID)
		require.Equal(t, team, m.TeamID)
		require.Equal(t, "default", m.ProjectID)
		require.Equal(t, 1, m.DefinitionVersion)
		require.Equal(t, id, m.RootExecutionID)
		require.Equal(t, "manual", m.Source)
		require.Len(t, m.SnapshotID, 64)
		require.False(t, m.IsTest)
		require.NotEqual(t, "default", m.Namespace)
		childID := state.ChildWorkflows["child"]
		require.NotEmpty(t, childID)
		cm := history(childID).Metadata
		require.NotNil(t, cm)
		require.Equal(t, m.Namespace, cm.Namespace)
		require.Equal(t, id, cm.RootExecutionID)
		require.Equal(t, "child", cm.Source)
		require.Equal(t, child.ID, cm.DefinitionID)
		require.Equal(t, folder["id"], cm.ProjectID)
		require.Equal(t, 2, cm.DefinitionVersion)
		require.NotEqual(t, m.SnapshotID, cm.SnapshotID)
		cfg, err := protection.TemporalOptions(address, m.Namespace)
		require.NoError(t, err)
		c, err := temporalclient.Dial(cfg)
		require.NoError(t, err)
		defer c.Close()
		desc, err := c.DescribeWorkflowExecution(ctx, id, record.TemporalRunID)
		require.NoError(t, err)
		fields := desc.WorkflowExecutionInfo.SearchAttributes.IndexedFields
		for key, expected := range map[string]string{"RunloomOrgId": org["id"], "RunloomTeamId": team, "RunloomProjectId": "default", "RunloomDefinitionId": parent.ID, "RunloomSource": "manual", "RunloomRootExecutionId": id, "RunloomSnapshotId": m.SnapshotID} {
			var actual string
			require.NoError(t, converter.GetDefaultDataConverter().FromPayload(fields[key], &actual))
			require.Equal(t, expected, actual)
		}
		var version int64
		var isTest bool
		require.NoError(t, converter.GetDefaultDataConverter().FromPayload(fields["RunloomDefinitionVersion"], &version))
		require.EqualValues(t, 1, version)
		require.NoError(t, converter.GetDefaultDataConverter().FromPayload(fields["RunloomIsTest"], &isTest))
		require.False(t, isTest)
		require.Eventually(t, func() bool {
			r, e := c.ListWorkflow(ctx, &workflowservice.ListWorkflowExecutionsRequest{Namespace: m.Namespace, Query: fmt.Sprintf("RunloomTeamId = '%s' AND RunloomRootExecutionId = '%s' AND RunloomDefinitionVersion = 1", team, id)})
			return e == nil && len(r.Executions) == 1
		}, 15*time.Second, 300*time.Millisecond)
		// Replay the real encrypted, tagged parent/child history.
		replayer, err := worker.NewWorkflowReplayerWithOptions(worker.WorkflowReplayerOptions{DataConverter: cfg.DataConverter, FailureConverter: cfg.FailureConverter})
		require.NoError(t, err)
		replayer.RegisterWorkflow(engine.RunloomWorkflow)
		for _, execution := range []string{id, childID} {
			events := &historypb.History{}
			it := c.GetWorkflowHistory(ctx, execution, "", false, enums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
			for it.HasNext() {
				e, err := it.Next()
				require.NoError(t, err)
				events.Events = append(events.Events, e)
			}
			require.NoError(t, replayer.ReplayWorkflowHistory(protection.SafeLogger{}, events))
		}
		if n == 0 {
			firstID, firstNamespace = id, m.Namespace
		} else {
			require.NotEqual(t, firstNamespace, m.Namespace)
			require.Equal(t, 404, call("GET", "/runs/"+firstID, nil, nil))
			_, err := c.DescribeWorkflowExecution(ctx, firstID, "")
			require.Error(t, err)
		}
		var testStart map[string]any
		call("POST", "/definitions/"+parent.ID+"/test", map[string]any{"definition": parent, "input": map[string]any{}}, &testStart)
		draftID := testStart["workflow_id"].(string)
		wait(draftID)
		dm := history(draftID).Metadata
		require.NotNil(t, dm)
		require.True(t, dm.IsTest)
		require.Zero(t, dm.DefinitionVersion)
		require.Equal(t, "draft_test", dm.Source)
		require.Len(t, dm.SnapshotID, 64)
		var schedule map[string]any
		call("POST", "/schedules", map[string]any{"name": "Namespace schedule", "definition_id": parent.ID, "version": 1, "interval_seconds": 3600, "paused": true}, &schedule)
		scheduleID := schedule["id"].(string)
		call("POST", "/schedules/"+scheduleID+"/trigger", map[string]any{}, &schedule)
		handle := c.ScheduleClient().GetHandle(ctx, scheduleID)
		var scheduledID string
		require.Eventually(t, func() bool {
			d, e := handle.Describe(ctx)
			if e != nil || len(d.Info.RecentActions) == 0 {
				return false
			}
			scheduledID = d.Info.RecentActions[0].StartWorkflowResult.WorkflowID
			return scheduledID != ""
		}, 20*time.Second, 300*time.Millisecond)
		wait(scheduledID)
		sm := history(scheduledID).Metadata
		require.NotNil(t, sm)
		require.Equal(t, scheduledID, sm.RootExecutionID)
		require.Equal(t, "schedule", sm.Source)
		require.Equal(t, m.Namespace, sm.Namespace)
		require.Equal(t, 200, call("DELETE", "/schedules/"+scheduleID, nil, nil))
	}
}
