package activities

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/google/uuid"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
)

// RunRecorder registers executions started by Temporal itself, before any user
// step can run. The team is bound to the actual Temporal workflow ID, never a
// workflow-supplied payload alone. Activity retries are idempotent.
type RunRecorder struct {
	OpenTeam func(context.Context, string) (RecordingStore, error)
}

type RecordingStore interface {
	store.Runs
	store.Audits
	Close(context.Context) error
}

func (a *RunRecorder) Record(ctx context.Context, input model.RunInput) error {
	return a.recordExecution(ctx, input, activity.GetInfo(ctx))
}

func (a *RunRecorder) recordExecution(ctx context.Context, input model.RunInput, info activity.Info) error {
	if input.Metadata != nil && (input.Metadata.Namespace != info.WorkflowNamespace || input.Metadata.TeamID != input.TeamID) {
		return temporal.NewNonRetryableApplicationError("execution namespace identity mismatch", "InvalidNamespace", nil)
	}
	team, err := uuid.Parse(input.TeamID)
	if err != nil || team.String() != input.TeamID || !strings.HasPrefix(info.WorkflowExecution.ID, input.TeamID+"-") {
		return temporal.NewNonRetryableApplicationError("run team does not match execution identity", "InvalidTeam", nil)
	}
	if input.Source != "schedule" && input.Source != "child" {
		return temporal.NewNonRetryableApplicationError("invalid run source", "InvalidSource", nil)
	}
	b, err := a.OpenTeam(ctx, input.TeamID)
	if err != nil {
		return err
	}
	defer b.Close(ctx)
	id := info.WorkflowExecution.ID
	triggerType := input.Source
	if input.IsTest {
		triggerType = "child_test"
	}
	previous, readErr := b.GetRun(id)
	if readErr != nil && !errors.Is(readErr, store.ErrNotFound) {
		return readErr
	}
	if readErr != nil {
		_, err = b.RecordRunStart(store.RunRecord{Metadata: input.Metadata, WorkflowID: id, TemporalRunID: info.WorkflowExecution.RunID,
			DefinitionID: input.Definition.ID, DefinitionVersion: input.Definition.Version, TriggerType: triggerType,
			Input: input.Input, TemporalStatus: "running"})
		if err != nil {
			previous, readErr = b.GetRun(id)
			if readErr != nil {
				return err
			}
		} else {
			previous.TemporalRunID = info.WorkflowExecution.RunID
		}
	}
	if previous.TemporalRunID != info.WorkflowExecution.RunID {
		return fmt.Errorf("workflow ID already belongs to another execution")
	}
	eventID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(id+":"+info.WorkflowExecution.RunID+":started")).String()
	_, err = b.AppendAudit(store.AuditEvent{EventID: eventID, WorkflowID: id, DefinitionID: input.Definition.ID,
		EventType: "workflow.started", Actor: input.Actor, Payload: map[string]any{
			"definition_version": input.Definition.Version, "trigger_type": triggerType, "is_test": input.IsTest,
			"parent_workflow_id": input.ParentWorkflowID, "schedule_id": input.ScheduleID,
		}})
	if err != nil {
		events, readErr := b.ListAudit(id, "", 1000)
		if readErr == nil {
			for _, event := range events {
				if event.EventID == eventID {
					err = nil
					break
				}
			}
		}
	}
	if err != nil {
		return err
	}
	// Keep a history entry even when a parent's mapped child input is invalid.
	if err := definition.ValidateInput(input.Definition.InputSchema, input.Input); err != nil {
		return temporal.NewNonRetryableApplicationError(err.Error(), "InvalidInput", err)
	}
	return nil
}
