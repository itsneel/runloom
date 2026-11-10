package store

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/example/runloom/workflows/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRunTagSearch(t *testing.T) {
	for _, driver := range []string{"file", "postgres", "mongodb"} {
		t.Run(driver, func(t *testing.T) {
			ctx := context.Background()
			var b Backend
			var err error
			switch driver {
			case "file":
				b, err = New(t.TempDir())
			case "postgres":
				url := os.Getenv("RUNLOOM_TEST_POSTGRES_URL")
				if url == "" {
					t.Skip("opt-in PostgreSQL")
				}
				b, err = NewPostgresTeam(ctx, url, "tags_"+uuid.NewString()[:8])
			case "mongodb":
				url := os.Getenv("RUNLOOM_TEST_MONGODB_URL")
				if url == "" {
					t.Skip("opt-in MongoDB")
				}
				b, err = NewMongoTeam(ctx, url, "runloom_tags_tests", uuid.NewString()+"_")
			}
			require.NoError(t, err)
			defer b.Close(ctx)
			for i := 0; i < 205; i++ {
				m := &model.ExecutionMetadata{OrgID: "org-one", TeamID: "team-one", ProjectID: "project-common", DefinitionID: "sample", DefinitionVersion: 12, Source: "manual", IsTest: false, SnapshotID: "snapshot", RootExecutionID: fmt.Sprintf("run-%03d", i)}
				if i == 0 {
					m.ProjectID = "special.%_[]"
					m.IsTest = true
					m.Source = "draft_test"
					m.DefinitionVersion = 0
				}
				_, err = b.RecordRunStart(RunRecord{WorkflowID: fmt.Sprintf("run-%03d", i), DefinitionID: "sample", TemporalStatus: "completed", Metadata: m, Input: map[string]any{"hidden": "not-searchable-payload"}, CreatedAt: time.Date(2026, 1, 1, 0, i, 0, 0, time.UTC).Format(time.RFC3339Nano)})
				require.NoError(t, err)
			}
			_, err = b.RecordRunStart(RunRecord{WorkflowID: "legacy", DefinitionID: "legacy", TemporalStatus: "completed"})
			require.NoError(t, err)
			find := func(f RunFilter) []RunRecord {
				f.Limit = 200
				rows, err := b.SearchRuns(ctx, f)
				require.NoError(t, err)
				return rows
			}
			require.Len(t, find(RunFilter{Search: "SPECIAL.%_[]"}), 1, "literal case-insensitive search reaches runs beyond latest 200")
			require.Len(t, find(RunFilter{TagKey: "project_id", TagValue: "special.%_[]"}), 1)
			require.Empty(t, find(RunFilter{TagKey: "project_id", TagValue: "SPECIAL.%_[]"}), "exact tags are case sensitive")
			require.Len(t, find(RunFilter{TagKey: "is_test", TagValue: "false"}), 200)
			require.Len(t, find(RunFilter{TagKey: "is_test", TagValue: "true"}), 1)
			require.Len(t, find(RunFilter{TagKey: "definition_version", TagValue: "0"}), 1)
			require.Len(t, find(RunFilter{Search: "rue"}), 1, "typed tag text supports substring search")
			require.Len(t, find(RunFilter{Search: "2"}), 200)
			require.Empty(t, find(RunFilter{Search: "not-searchable-payload"}))
			require.Empty(t, find(RunFilter{TagKey: "org_id", TagValue: "org-other"}))
			require.Empty(t, find(RunFilter{Search: "special", TagKey: "source", TagValue: "manual"}))
			first := find(RunFilter{TagKey: "project_id", TagValue: "project-common"})
			second := find(RunFilter{TagKey: "project_id", TagValue: "project-common", Offset: 200})
			require.Len(t, first, 200)
			require.Len(t, second, 4)
			ids := map[string]bool{}
			for _, r := range append(first, second...) {
				require.False(t, ids[r.WorkflowID])
				ids[r.WorkflowID] = true
			}
			require.Len(t, find(RunFilter{Search: "legacy"}), 1)
			for _, f := range []RunFilter{{TagKey: "input", TagValue: "secret"}, {TagKey: "is_test", TagValue: "yes"}, {TagKey: "definition_version", TagValue: "1 OR 1=1"}, {TagKey: "source"}, {Offset: -1}, {TagKey: "source'", TagValue: "x"}} {
				f.Limit = 200
				_, err := b.SearchRuns(ctx, f)
				require.Error(t, err)
			}
		})
	}
}
