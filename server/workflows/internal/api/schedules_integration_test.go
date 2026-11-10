package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/example/runloom/workflows/internal/activities"
	"github.com/example/runloom/workflows/internal/engine"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

// Opt in against an isolated local Temporal server. This creates only synthetic
// definitions and runs in a random team and task queue; it never calls an LLM.
func TestSchedulesAndChildrenLive(t *testing.T) {
	address := os.Getenv("RUNLOOM_TEST_TEMPORAL_ADDRESS")
	if address == "" {
		t.Skip("set RUNLOOM_TEST_TEMPORAL_ADDRESS for live Temporal checks")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c, err := client.Dial(client.Options{HostPort: address})
	require.NoError(t, err)
	defer c.Close()
	team := uuid.NewString()
	queue := "runloom-test-" + team
	data := t.TempDir()
	openTeam := func(ctx context.Context, id string) (store.Backend, error) {
		return store.OpenTeam(ctx, os.Getenv("RUNLOOM_TEST_DATABASE_DRIVER"), os.Getenv("RUNLOOM_TEST_DATABASE_URL"), "runloom_schedule_tests", data, id)
	}
	b, err := openTeam(ctx, team)
	require.NoError(t, err)
	defer b.Close(context.Background())
	recorder := activities.RunRecorder{OpenTeam: func(ctx context.Context, id string) (activities.RecordingStore, error) { return openTeam(ctx, id) }}
	w := worker.New(c, queue, worker.Options{})
	w.RegisterWorkflow(engine.RunloomWorkflow)
	w.RegisterWorkflow(engine.RunloomChildTestWorkflow)
	w.RegisterWorkflow(engine.RunloomDraftTestWorkflow)
	w.RegisterActivityWithOptions(recorder.Record, activity.RegisterOptions{Name: "runloom.internal.record_run"})
	w.RegisterActivityWithOptions(activities.Echo, activity.RegisterOptions{Name: "runloom.echo"})
	require.NoError(t, w.Start())
	defer w.Stop()
	s := New(b, c, queue)
	defer s.Close()
	call := func(method, path string, body any, status int) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(method, path, strings.NewReader(string(raw))).WithContext(ctx)
		req.Header.Set("X-Runloom-Team", team)
		req.Header.Set("X-Runloom-Actor", "integration-tester")
		result := httptest.NewRecorder()
		s.Handler().ServeHTTP(result, req)
		require.Equal(t, status, result.Code, result.Body.String())
		return result
	}
	child := model.Definition{SchemaVersion: "1.0", ID: "child", Name: "Child", InputSchema: map[string]any{"type": "object", "required": []any{"value"}}, Steps: []model.Step{{ID: "gate", Type: "callback", CallbackKey: "ready", Prompt: "Await callback", OnTimeout: "fail"}}, Output: map[string]any{"value": "${input.value}", "approved": "${steps.gate.output.payload.approved}"}}
	child.Kind = "child"
	parent := model.Definition{SchemaVersion: "1.0", ID: "parent", Name: "Parent", Steps: []model.Step{{ID: "child-call", Type: "subworkflow", DefinitionID: "child", Version: 1, With: map[string]any{"value": "${input.value}"}}}, Output: "${steps.child-call.output}"}
	publish := func(value model.Definition) {
		call("POST", "/api/v1/studio/definitions", value, 200)
		call("POST", "/api/v1/studio/definitions/"+value.ID+"/publish", map[string]any{}, 200)
	}
	publish(child)
	publish(parent)
	call("POST", "/api/v1/studio/runs", map[string]any{"definition_id": "child", "input": map[string]any{"value": "blocked"}}, 422)
	call("POST", "/api/v1/studio/webhooks/child/anything", map[string]any{}, 422)
	call("POST", "/api/v1/studio/schedules", scheduleRequest{Name: "Blocked child", DefinitionID: "child", Version: 1, IntervalSeconds: 60, Input: map[string]any{"value": "blocked"}}, 422)
	// Studio tests have an independent workflow ID and retain callback controls.
	testResponse := call("POST", "/api/v1/studio/definitions/child/test", map[string]any{"version": 1, "input": map[string]any{"value": "studio-test"}}, 201)
	var testRun struct {
		ID string `json:"workflow_id"`
	}
	require.NoError(t, json.Unmarshal(testResponse.Body.Bytes(), &testRun))
	call("POST", "/api/v1/studio/callbacks/"+testRun.ID+"/gate", map[string]any{"payload": map[string]any{"approved": true}}, 200)
	var testResult model.RunResult
	require.NoError(t, c.GetWorkflow(ctx, testRun.ID, "").Get(ctx, &testResult))
	require.Equal(t, "studio-test", testResult.Output.(map[string]any)["value"])
	call("GET", "/api/v1/studio/runs/"+testRun.ID, nil, 200)
	testRecord, err := b.GetRun(testRun.ID)
	require.NoError(t, err)
	require.Equal(t, "child_test", testRecord.TriggerType)
	require.True(t, testRecord.State.IsTest)
	require.Empty(t, testRecord.State.ParentWorkflowID)
	testHistory := &historypb.History{}
	testIterator := c.GetWorkflowHistory(ctx, testRun.ID, "", false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	for testIterator.HasNext() {
		event, err := testIterator.Next()
		require.NoError(t, err)
		testHistory.Events = append(testHistory.Events, event)
	}
	testReplay := worker.NewWorkflowReplayer()
	testReplay.RegisterWorkflow(engine.RunloomChildTestWorkflow)
	require.NoError(t, testReplay.ReplayWorkflowHistory(nil, testHistory))
	for _, kind := range []string{"main", "child"} {
		draft := model.Definition{SchemaVersion: "1.0", ID: "draft-" + kind, Kind: kind, Name: "Unpublished test", Steps: []model.Step{{ID: "echo", Type: "activity", Activity: "runloom.echo", With: map[string]any{"value": "${input.value}"}}}, Output: "${steps.echo.output}"}
		_, err := b.SaveDraft(draft)
		require.NoError(t, err)
		response := call("POST", "/api/v1/studio/definitions/"+draft.ID+"/test", map[string]any{"definition": draft, "input": map[string]any{"value": "draft-snapshot"}}, 201)
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &testRun))
		require.NoError(t, c.GetWorkflow(ctx, testRun.ID, "").Get(ctx, &testResult))
		require.Equal(t, "draft-snapshot", testResult.Output.(map[string]any)["value"])
		call("GET", "/api/v1/studio/runs/"+testRun.ID, nil, 200)
		record, err := b.GetRun(testRun.ID)
		require.NoError(t, err)
		require.Equal(t, "draft_test", record.TriggerType)
		require.True(t, record.State.IsTest)
		history := &historypb.History{}
		it := c.GetWorkflowHistory(ctx, testRun.ID, "", false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
		for it.HasNext() {
			event, err := it.Next()
			require.NoError(t, err)
			history.Events = append(history.Events, event)
		}
		replay := worker.NewWorkflowReplayer()
		replay.RegisterWorkflow(engine.RunloomDraftTestWorkflow)
		require.NoError(t, replay.ReplayWorkflowHistory(nil, history))
	}
	create := func(body scheduleRequest) scheduleView {
		response := call("POST", "/api/v1/studio/schedules", body, 201)
		var view scheduleView
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &view))
		return view
	}
	// Clean up before the client closes (t.Cleanup runs after defers).
	var scheduleIDs []string
	defer func() {
		for _, id := range scheduleIDs {
			_ = c.ScheduleClient().GetHandle(context.Background(), id).Delete(context.Background())
		}
	}()
	scheduled := create(scheduleRequest{Name: "Paused interval", DefinitionID: "parent", Version: 1, IntervalSeconds: 3600, Paused: true, Input: map[string]any{"value": "pinned"}})
	scheduleIDs = append(scheduleIDs, scheduled.ID)
	calendar := create(scheduleRequest{Name: "Calendar", DefinitionID: "parent", Version: 1, Cron: "0 9 * * 1-5", TimeZone: "Asia/Kolkata", Paused: true, Input: map[string]any{"value": "calendar"}})
	scheduleIDs = append(scheduleIDs, calendar.ID)
	calendarDescription, err := c.ScheduleClient().GetHandle(ctx, calendar.ID).Describe(ctx)
	require.NoError(t, err)
	require.Equal(t, "Asia/Kolkata", calendarDescription.Schedule.Spec.TimeZoneName)
	call("POST", "/api/v1/studio/schedules", scheduleRequest{Name: "Invalid cron", DefinitionID: "parent", Version: 1, Cron: "99 99 * * *"}, 422)
	// Later child publication must not change this schedule's pinned dependency.
	child.Output = map[string]any{"value": "NEW VERSION"}
	publish(child)
	d, err := c.ScheduleClient().GetHandle(ctx, scheduled.ID).Describe(ctx)
	require.NoError(t, err)
	require.True(t, d.Schedule.State.Paused)
	require.Equal(t, enumspb.SCHEDULE_OVERLAP_POLICY_SKIP, d.Schedule.Policy.Overlap)
	require.True(t, d.Schedule.Policy.PauseOnFailure)
	require.Equal(t, 15*time.Minute, d.Schedule.Policy.CatchupWindow)
	call("POST", "/api/v1/studio/schedules/"+scheduled.ID+"/trigger", nil, 200)
	var parentID, childID string
	require.Eventually(t, func() bool {
		runs, err := b.ListRuns(100)
		if err != nil {
			return false
		}
		for _, run := range runs {
			if run.TriggerType == "schedule" {
				parentID = run.WorkflowID
			}
			if run.TriggerType == "child" {
				childID = run.WorkflowID
			}
		}
		return parentID != "" && childID != ""
	}, 20*time.Second, 100*time.Millisecond)
	childRecord, err := b.GetRun(childID)
	require.NoError(t, err)
	require.Equal(t, 1, childRecord.DefinitionVersion)
	require.Equal(t, "pinned", childRecord.Input["value"])
	require.Eventually(t, func() bool {
		v, err := c.QueryWorkflow(ctx, childID, "", "status")
		if err != nil {
			return false
		}
		var state model.RunState
		if v.Get(&state) != nil {
			return false
		}
		return len(state.WaitingCallbacks) == 1
	}, 10*time.Second, 100*time.Millisecond)
	call("GET", "/api/v1/studio/runs/"+childID, nil, 200)
	call("POST", "/api/v1/studio/schedules/"+scheduled.ID+"/trigger", nil, 200)
	require.Eventually(t, func() bool {
		v, err := c.ScheduleClient().GetHandle(ctx, scheduled.ID).Describe(ctx)
		return err == nil && v.Info.NumActionsSkippedOverlap > 0
	}, 10*time.Second, 100*time.Millisecond)
	call("POST", "/api/v1/studio/callbacks/"+childID+"/gate", map[string]any{"payload": map[string]any{"approved": true}}, 200)
	var result model.RunResult
	require.NoError(t, c.GetWorkflow(ctx, parentID, "").Get(ctx, &result))
	require.Equal(t, "pinned", result.Output.(map[string]any)["value"])
	require.Equal(t, true, result.Output.(map[string]any)["approved"])
	for _, id := range []string{parentID, childID} {
		call("GET", "/api/v1/studio/runs/"+id, nil, 200)
		events, err := b.ListAudit(id, "", 100)
		require.NoError(t, err)
		started := 0
		for _, event := range events {
			if event.EventType == "workflow.started" {
				started++
			}
		}
		require.Equal(t, 1, started)
		history := &historypb.History{}
		it := c.GetWorkflowHistory(ctx, id, "", false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
		for it.HasNext() {
			event, err := it.Next()
			require.NoError(t, err)
			history.Events = append(history.Events, event)
		}
		replay := worker.NewWorkflowReplayer()
		replay.RegisterWorkflow(engine.RunloomWorkflow)
		require.NoError(t, replay.ReplayWorkflowHistory(nil, history))
	}
	stored, err := b.GetRun(parentID)
	require.NoError(t, err)
	require.Equal(t, childID, stored.State.ChildWorkflows["child-call"])
	call("POST", "/api/v1/studio/schedules/"+scheduled.ID+"/resume", nil, 200)
	d, err = c.ScheduleClient().GetHandle(ctx, scheduled.ID).Describe(ctx)
	require.NoError(t, err)
	require.False(t, d.Schedule.State.Paused)
	call("POST", "/api/v1/studio/schedules/"+scheduled.ID+"/pause", nil, 200)
	call("GET", "/api/v1/studio/schedules/"+scheduled.ID, nil, 200)
	require.Eventually(t, func() bool {
		return strings.Contains(call("GET", "/api/v1/studio/schedules", nil, 200).Body.String(), scheduled.ID)
	}, 10*time.Second, 200*time.Millisecond)
	// One-time execution must start automatically, not through Trigger.
	simple := model.Definition{SchemaVersion: "1.0", ID: "simple", Name: "Simple", Steps: []model.Step{{ID: "echo", Type: "activity", Activity: "runloom.echo", With: map[string]any{"ok": true}}}, Output: "${steps.echo.output}"}
	publish(simple)
	once := create(scheduleRequest{Name: "Once", DefinitionID: "simple", Version: 1, At: time.Now().Add(3 * time.Second).Format(time.RFC3339)})
	scheduleIDs = append(scheduleIDs, once.ID)
	var onceID string
	require.Eventually(t, func() bool {
		runs, err := b.ListRuns(100)
		if err != nil || len(runs) == 0 {
			return false
		}
		for _, run := range runs {
			if run.DefinitionID == "simple" {
				onceID = run.WorkflowID
				return true
			}
		}
		return false
	}, 15*time.Second, 100*time.Millisecond)
	require.NoError(t, c.GetWorkflow(ctx, onceID, "").Get(ctx, &result))
	require.Equal(t, true, result.Output.(map[string]any)["ok"])
	// Parent cancellation closes its active child, including callback waits.
	started := call("POST", "/api/v1/studio/runs", map[string]any{"definition_id": "parent", "version": 1, "input": map[string]any{"value": "cancel"}}, 201)
	var run struct {
		ID string `json:"workflow_id"`
	}
	require.NoError(t, json.Unmarshal(started.Body.Bytes(), &run))
	var cancelChild string
	require.Eventually(t, func() bool {
		q, err := c.QueryWorkflow(ctx, run.ID, "", "status")
		if err != nil {
			return false
		}
		var state model.RunState
		if q.Get(&state) != nil {
			return false
		}
		cancelChild = state.ChildWorkflows["child-call"]
		if cancelChild == "" {
			return false
		}
		_, err = b.GetRun(cancelChild)
		return err == nil
	}, 15*time.Second, 100*time.Millisecond)
	call("POST", "/api/v1/studio/runs/"+run.ID+"/cancel", nil, 200)
	require.Error(t, c.GetWorkflow(ctx, run.ID, "").Get(ctx, nil))
	require.Eventually(t, func() bool {
		d, err := c.DescribeWorkflowExecution(ctx, cancelChild, "")
		return err == nil && d.WorkflowExecutionInfo.Status != enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING
	}, 10*time.Second, 100*time.Millisecond)
	// Invalid mapped input remains in history, but no user child step runs.
	parent.ID = "invalid-parent"
	parent.Steps[0].With = map[string]any{}
	publish(parent)
	started = call("POST", "/api/v1/studio/runs", map[string]any{"definition_id": "invalid-parent", "input": map[string]any{}}, 201)
	require.NoError(t, json.Unmarshal(started.Body.Bytes(), &run))
	require.ErrorContains(t, c.GetWorkflow(ctx, run.ID, "").Get(ctx, nil), "InvalidInput")
	call("DELETE", "/api/v1/studio/schedules/"+scheduled.ID, nil, http.StatusOK)
	call("GET", "/api/v1/studio/schedules/"+scheduled.ID, nil, http.StatusNotFound)
}
