package store

import (
	"context"
	"github.com/example/runloom/workflows/internal/usage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestUsageLedger(t *testing.T) {
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
					t.Skip("set RUNLOOM_TEST_POSTGRES_URL for live PostgreSQL")
				}
				b, err = NewPostgresTeam(ctx, url, "usage_test_"+uuid.NewString()[:8])
			case "mongodb":
				url := os.Getenv("RUNLOOM_TEST_MONGODB_URL")
				if url == "" {
					t.Skip("set RUNLOOM_TEST_MONGODB_URL for live MongoDB")
				}
				b, err = NewMongoTeam(ctx, url, "runloom_usage_tests", uuid.NewString()+"_")
			}
			require.NoError(t, err)
			defer b.Close(ctx)
			root := usage.RunKey("parent", "run")
			row := usage.Entry{ID: uuid.NewString(), WorkflowID: "child", TemporalRunID: "child-run", Ancestors: []string{root, usage.RunKey("child", "child-run")}, Status: "pending", Accounting: "pending", Model: "test"}
			created, err := b.BeginUsage(ctx, row)
			require.NoError(t, err)
			require.True(t, created)
			created, err = b.BeginUsage(ctx, row)
			require.NoError(t, err)
			require.False(t, created)
			row.Status = "succeeded"
			row.Accounting = "estimated"
			row.CostUSD = "0.000000000001"
			require.NoError(t, b.FinishUsage(ctx, row))
			row.CostUSD = "999"
			require.NoError(t, b.FinishUsage(ctx, row))
			values, err := b.QueryUsage(ctx, usage.Filter{RunKey: root})
			require.NoError(t, err)
			require.Len(t, values, 1)
			require.Equal(t, "0.000000000001", values[0].CostUSD)
			values, err = b.QueryUsage(ctx, usage.Filter{RunKey: root, After: row.ID})
			require.NoError(t, err)
			require.Empty(t, values)
			values, err = b.QueryUsage(ctx, usage.Filter{RunKey: "other-team"})
			require.NoError(t, err)
			require.Empty(t, values)
			values, err = b.QueryUsage(ctx, usage.Filter{RunKey: root, Model: "other"})
			require.NoError(t, err)
			require.Empty(t, values)
			// UTC second-prefix bounds include all fractions of the starting
			// second, and exclude the entire ending second in every backend.
			for _, at := range []string{"2026-10-01T00:00:00Z", "2026-10-01T00:00:00.000000001Z", "2026-10-01T23:59:59.999999999Z", "2026-10-02T00:00:00Z", "2026-09-30T23:59:59Z"} {
				entry := usage.Entry{ID: uuid.NewString(), StartedAt: at, Status: "pending", Accounting: "pending"}
				_, err = b.BeginUsage(ctx, entry)
				require.NoError(t, err)
			}
			f := usage.Filter{All: true, StartedFrom: "2026-10-01T00:00:00", StartedBefore: "2026-10-02T00:00:00", Limit: 10}
			values, err = b.QueryUsage(ctx, f)
			require.NoError(t, err)
			require.Len(t, values, 3)
			f.Limit = 1
			first, err := b.QueryUsage(ctx, f)
			require.NoError(t, err)
			require.Len(t, first, 1)
			f.After = first[0].ID
			f.Limit = 10
			rest, err := b.QueryUsage(ctx, f)
			require.NoError(t, err)
			require.Len(t, rest, 2)
			f.All = false
			f.After = ""
			values, err = b.QueryUsage(ctx, f)
			require.NoError(t, err)
			require.Empty(t, values, "empty run key must not imply team-wide access")
		})
	}
}
