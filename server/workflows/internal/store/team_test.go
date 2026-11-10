package store

import (
	"context"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestTeamIsolation(t *testing.T) {
	for _, test := range []struct{ driver, variable string }{{"file", ""}, {"postgres", "RUNLOOM_TEST_POSTGRES_URL"}, {"mongodb", "RUNLOOM_TEST_MONGODB_URL"}} {
		t.Run(test.driver, func(t *testing.T) {
			url := os.Getenv(test.variable)
			if test.variable != "" && url == "" {
				t.Skip("disposable database URL not configured")
			}
			ctx := context.Background()
			root := t.TempDir()
			first, err := OpenTeam(ctx, test.driver, url, "runloom_team_test", root, uuid.NewString())
			require.NoError(t, err)
			defer first.Close(ctx)
			second, err := OpenTeam(ctx, test.driver, url, "runloom_team_test", root, uuid.NewString())
			require.NoError(t, err)
			defer second.Close(ctx)
			legacy, err := Open(ctx, test.driver, url, "runloom_team_test", root)
			require.NoError(t, err)
			defer legacy.Close(ctx)
			value := model.Definition{SchemaVersion: "1.0", ID: "same-id", Name: "First team", Steps: []model.Step{{ID: "echo", Type: "activity", Activity: "runloom.echo"}}}
			_, err = first.SaveDraft(value)
			require.NoError(t, err)
			_, err = first.Publish(value.ID)
			require.NoError(t, err)
			for _, other := range []Backend{second, legacy} {
				_, err = other.Get(value.ID, nil)
				require.ErrorIs(t, err, ErrNotFound)
				commits, err := other.ListCommits(value.ID, 10)
				require.NoError(t, err)
				require.Empty(t, commits)
			}
			value.Name = "Second team"
			project, err := first.CreateProject(ctx, "Team-only project")
			require.NoError(t, err)
			require.NoError(t, first.AssignProject(ctx, value.ID, project.ID))
			for _, other := range []Backend{second, legacy} {
				catalog, err := other.ProjectCatalog(ctx)
				require.NoError(t, err)
				require.NotContains(t, catalog.Projects, project.ID)
				require.Equal(t, DefaultProjectID, catalog.WorkflowProject(value.ID))
				_, err = other.RenameProject(ctx, project.ID, "Unauthorized")
				require.ErrorIs(t, err, ErrProjectNotFound)
			}
			_, err = second.SaveDraft(value)
			require.NoError(t, err)
			got, err := first.Get(value.ID, nil)
			require.NoError(t, err)
			require.Equal(t, "First team", got.Name)
			_, err = first.RecordRunStart(RunRecord{WorkflowID: "same-run", TemporalStatus: "running", Input: map[string]any{}})
			require.NoError(t, err)
			_, err = second.GetRun("same-run")
			require.Error(t, err)
			_, err = first.AppendAudit(AuditEvent{WorkflowID: "same-run", Actor: "first-team", EventType: "test"})
			require.NoError(t, err)
			events, err := second.ListAudit("same-run", "", 10)
			require.NoError(t, err)
			require.Empty(t, events)
			id := uuid.NewString()
			_, err = first.AppendAudit(AuditEvent{EventID: id, WorkflowID: "retry-run", EventType: "workflow.started", Actor: "original"})
			require.NoError(t, err)
			_, err = first.AppendAudit(AuditEvent{EventID: id, WorkflowID: "retry-run", EventType: "workflow.started", Actor: "must-not-overwrite"})
			require.NoError(t, err)
			events, err = first.ListAudit("retry-run", "", 10)
			require.NoError(t, err)
			require.Len(t, events, 1)
			require.Equal(t, "original", events[0].Actor)
		})
	}
}
