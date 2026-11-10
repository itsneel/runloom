package activities

import (
	"context"
	"testing"

	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/workflow"
)

func TestRunRegistrationIdentityAndIdempotence(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	team := uuid.NewString()
	opened := 0
	recorder := RunRecorder{OpenTeam: func(ctx context.Context, id string) (RecordingStore, error) {
		opened++
		return store.OpenTeam(ctx, "file", "", "", dir, id)
	}}
	input := model.RunInput{TeamID: team, Source: "child", Actor: "tester", ParentWorkflowID: team + "-parent", Definition: model.Definition{ID: "child", Version: 1, InputSchema: map[string]any{"type": "object", "required": []any{"name"}}}, Input: map[string]any{"name": "Avery"}}
	info := activity.Info{WorkflowExecution: workflow.Execution{ID: team + "-child", RunID: uuid.NewString()}}
	wrongNamespace := input
	wrongNamespace.Metadata = &model.ExecutionMetadata{Namespace: "another-org", TeamID: team}
	require.ErrorContains(t, recorder.recordExecution(ctx, wrongNamespace, info), "InvalidNamespace")
	require.Zero(t, opened, "namespace mismatch must fail before opening a database")
	spoofed := input
	spoofed.TeamID = uuid.NewString()
	require.ErrorContains(t, recorder.recordExecution(ctx, spoofed, info), "InvalidTeam")
	require.Zero(t, opened)
	spoofed = input
	spoofed.Source = "manual"
	require.ErrorContains(t, recorder.recordExecution(ctx, spoofed, info), "InvalidSource")
	require.Zero(t, opened)
	require.NoError(t, recorder.recordExecution(ctx, input, info))
	require.NoError(t, recorder.recordExecution(ctx, input, info))
	b, err := store.OpenTeam(ctx, "file", "", "", dir, team)
	require.NoError(t, err)
	defer b.Close(ctx)
	events, err := b.ListAudit(info.WorkflowExecution.ID, "", 100)
	require.NoError(t, err)
	require.Len(t, events, 1)
	run, err := b.GetRun(info.WorkflowExecution.ID)
	require.NoError(t, err)
	require.Equal(t, "Avery", run.Input["name"])
	testInfo := info
	testInfo.WorkflowExecution.ID = team + "-test-child"
	testInput := input
	testInput.IsTest = true
	require.NoError(t, recorder.recordExecution(ctx, testInput, testInfo))
	testRun, err := b.GetRun(testInfo.WorkflowExecution.ID)
	require.NoError(t, err)
	require.Equal(t, "child_test", testRun.TriggerType, "nested test children must remain labeled in history")
	info.WorkflowExecution.RunID = uuid.NewString()
	require.ErrorContains(t, recorder.recordExecution(ctx, input, info), "another execution")
	info.WorkflowExecution.ID = team + "-invalid"
	input.Input = map[string]any{}
	require.ErrorContains(t, recorder.recordExecution(ctx, input, info), "InvalidInput")
	_, err = b.GetRun(info.WorkflowExecution.ID)
	require.NoError(t, err, "invalid child input must remain visible in run history")
}
