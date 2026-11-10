package store

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
)

func exampleDefinition(t *testing.T) model.Definition {
	t.Helper()
	data, err := os.ReadFile("../../testdata/examples/employee-onboarding.json")
	require.NoError(t, err)
	var value model.Definition
	require.NoError(t, json.Unmarshal(data, &value))
	return value
}

func TestDraftAndPublishedVersionsAreIndependent(t *testing.T) {
	definitions, err := New(t.TempDir())
	require.NoError(t, err)
	value := exampleDefinition(t)

	_, err = definitions.SaveDraft(value)
	require.NoError(t, err)
	one, err := definitions.Publish(value.ID)
	require.NoError(t, err)
	value.Name = "Edited name"
	_, err = definitions.SaveDraft(value)
	require.NoError(t, err)
	two, err := definitions.Publish(value.ID)
	require.NoError(t, err)

	require.Equal(t, 1, one.Version)
	require.Equal(t, 2, two.Version)
	versionOne := 1
	stored, err := definitions.Get(value.ID, &versionOne)
	require.NoError(t, err)
	require.Equal(t, "Employee onboarding", stored.Name)
	commits, err := definitions.ListCommits(value.ID, 10)
	require.NoError(t, err)
	require.Equal(t, []string{"publish", "save", "publish", "save"}, []string{commits[0].Kind, commits[1].Kind, commits[2].Kind, commits[3].Kind})
	source, err := definitions.GetCommit(value.ID, commits[3].CommitID)
	require.NoError(t, err)
	require.NotNil(t, source.Body)
	restored, restoreCommit, err := definitions.RestoreCommit(value.ID, source.CommitID, CommitMeta{Actor: "reviewer", Message: "Restore original"})
	require.NoError(t, err)
	require.Equal(t, 0, restored.Version)
	require.Equal(t, "Employee onboarding", restored.Name)
	require.Equal(t, "restore", restoreCommit.Kind)
	require.Equal(t, int64(5), restoreCommit.Sequence)
}

func TestRunInputsStatusOutputAndAuditHistoryArePersisted(t *testing.T) {
	values, err := New(t.TempDir())
	require.NoError(t, err)
	run, err := values.RecordRunStart(RunRecord{
		WorkflowID:        "run-1",
		TemporalRunID:     "temporal-run-1",
		DefinitionID:      "employee-onboarding",
		DefinitionVersion: 2,
		TriggerType:       "webhook",
		Input:             map[string]any{"employee_name": "Avery"},
		TemporalStatus:    "running",
	})
	require.NoError(t, err)
	require.NotEmpty(t, run.CreatedAt)
	_, err = values.AppendAudit(AuditEvent{WorkflowID: "run-1", DefinitionID: "employee-onboarding", EventType: "workflow.started", Actor: "webhook", Payload: map[string]any{"definition_version": 2}})
	require.NoError(t, err)

	state := &model.RunState{Status: "completed", Output: map[string]any{"ok": true}}
	changed, err := values.UpdateRun("run-1", "completed", state)
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = values.UpdateRun("run-1", "completed", state)
	require.NoError(t, err)
	require.False(t, changed)

	stored, err := values.GetRun("run-1")
	require.NoError(t, err)
	require.Equal(t, "Avery", stored.Input["employee_name"])
	require.Equal(t, true, stored.Output.(map[string]any)["ok"])
	require.NotNil(t, stored.CompletedAt)
	runs, err := values.ListRuns(50)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	events, err := values.ListAudit("run-1", "", 200)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "workflow.started", events[0].EventType)
}
