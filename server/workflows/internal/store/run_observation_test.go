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

func TestRunObservationOrderingAndFullPagination(t *testing.T) {
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
				b, err = NewPostgresTeam(ctx, url, "observations_"+uuid.NewString()[:8])
			case "mongodb":
				url := os.Getenv("RUNLOOM_TEST_MONGODB_URL")
				if url == "" {
					t.Skip("opt-in MongoDB")
				}
				b, err = NewMongoTeam(ctx, url, "runloom_observation_tests", uuid.NewString()+"_")
			}
			require.NoError(t, err)
			defer b.Close(ctx)
			for i := 0; i < 251; i++ {
				_, err = b.RecordRunStart(RunRecord{WorkflowID: fmt.Sprintf("run-%03d", i), TemporalStatus: "running"})
				require.NoError(t, err)
			}
			seen := map[string]bool{}
			after := ""
			for {
				rows, err := b.PendingRuns(ctx, after, 100)
				require.NoError(t, err)
				for _, r := range rows {
					require.False(t, seen[r.WorkflowID])
					seen[r.WorkflowID] = true
					after = r.WorkflowID
				}
				if len(rows) < 100 {
					break
				}
			}
			require.Len(t, seen, 251)
			now := time.Now().UTC()
			state := &model.RunState{Status: "paused", Output: "preserved"}
			changed, err := b.ObserveRun(ctx, "run-000", "running", state, now)
			require.NoError(t, err)
			require.True(t, changed)
			changed, err = b.ObserveRun(ctx, "run-000", "running", state, now.Add(3*time.Second))
			require.NoError(t, err)
			require.False(t, changed)
			changed, err = b.ObserveRun(ctx, "run-000", "running", &model.RunState{Status: "running"}, now.Add(time.Second))
			require.NoError(t, err)
			require.False(t, changed, "unchanged newer observation must advance watermark")
			_, err = b.ObserveRun(ctx, "run-000", "completed", nil, now.Add(4*time.Second))
			require.NoError(t, err)
			changed, err = b.ObserveRun(ctx, "run-000", "running", nil, now.Add(5*time.Second))
			require.NoError(t, err)
			require.False(t, changed, "terminal runs cannot regress")
			run, err := b.GetRun("run-000")
			require.NoError(t, err)
			require.Equal(t, "completed", run.TemporalStatus)
			require.Equal(t, "preserved", run.Output)
			rows, err := b.PendingRuns(ctx, "", 1000)
			require.NoError(t, err)
			require.Len(t, rows, 250)
		})
	}
}
