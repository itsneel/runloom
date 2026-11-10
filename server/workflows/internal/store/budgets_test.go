package store

import (
	"context"
	"github.com/example/runloom/workflows/internal/budget"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/usage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProjectBudgets(t *testing.T) {
	for _, driver := range []string{"file", "postgres", "mongodb"} {
		t.Run(driver, func(t *testing.T) {
			ctx := context.Background()
			var b Backend
			var err error
			switch driver {
			case "file":
				b, err = New(t.TempDir())
			case "postgres":
				if os.Getenv("RUNLOOM_TEST_POSTGRES_URL") == "" {
					t.Skip("opt-in PostgreSQL")
				}
				b, err = NewPostgresTeam(ctx, os.Getenv("RUNLOOM_TEST_POSTGRES_URL"), "budgets_"+uuid.NewString()[:8])
			case "mongodb":
				if os.Getenv("RUNLOOM_TEST_MONGODB_URL") == "" {
					t.Skip("opt-in MongoDB")
				}
				b, err = NewMongoTeam(ctx, os.Getenv("RUNLOOM_TEST_MONGODB_URL"), "runloom_budget_tests", uuid.NewString()+"_")
			}
			require.NoError(t, err)
			defer b.Close(ctx)
			_, err = b.SaveDraft(model.Definition{SchemaVersion: "1.0", ID: "workflow", Name: "Workflow", Steps: []model.Step{{ID: "echo", Type: "activity", Activity: "echo"}}})
			require.NoError(t, err)
			p, err := b.SaveBudgetPolicy(ctx, budget.Policy{ID: uuid.NewString(), Name: "Shared rules", Enabled: true, SoftUSD: "1", HardUSD: "2", Reason: "test"}, "admin")
			require.NoError(t, err)
			other, err := b.CreateProject(ctx, "Other")
			require.NoError(t, err)
			now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
			for _, row := range []struct{ project, cost, at string }{{"default", "2", "2026-10-01T00:00:00.000000001Z"}, {other.ID, "1", "2026-10-31T23:59:59Z"}, {"default", "999", "2026-09-30T23:59:59Z"}, {"default", "999", "2026-11-01T00:00:00Z"}} {
				e := usage.Entry{ID: uuid.NewString(), ProjectID: row.project, DefinitionID: "workflow", Status: "pending", Accounting: "pending", StartedAt: row.at, Ancestors: []string{"parent:run", "child:run"}}
				_, err = b.BeginUsage(ctx, e)
				require.NoError(t, err)
				e.Status = "succeeded"
				e.Accounting = "estimated"
				e.CostUSD = row.cost
				require.NoError(t, b.FinishUsage(ctx, e))
			}
			project, decisions, err := CheckLLMBudget(ctx, b, "workflow", now)
			require.NoError(t, err)
			require.Equal(t, "default", project)
			require.Len(t, decisions, 2)
			require.Equal(t, "2.000000000000", decisions[1].SpendUSD)
			require.NoError(t, b.AssignProject(ctx, "workflow", other.ID))
			_, decisions, err = CheckLLMBudget(ctx, b, "workflow", now)
			require.NoError(t, err)
			require.Len(t, decisions, 1, "separate per-project allowance; moving does not transfer attributed spend")
			p.ExcludedWorkflows = []string{"workflow"}
			p, err = b.SaveBudgetPolicy(ctx, p, "admin")
			require.NoError(t, err)
			_, decisions, err = CheckLLMBudget(ctx, b, "workflow", now)
			require.NoError(t, err)
			require.Empty(t, decisions)
			c, err := b.ProjectCatalog(ctx)
			require.NoError(t, err)
			totals, err := MonthlyBudgetSpend(ctx, b, c, now)
			require.NoError(t, err)
			require.Equal(t, "1.000000000000", totals[other.ID].SpendUSD, "exclusion bypasses enforcement, not accounting")
			_, err = b.SaveBudgetPolicy(ctx, budget.Policy{ID: p.ID, Revision: 0, Name: "Stale", Enabled: true, HardUSD: "999", Reason: "stale"}, "other-admin")
			require.ErrorIs(t, err, ErrBudgetConflict)
			require.Len(t, c.BudgetChanges, 2)
			require.Equal(t, "admin", c.BudgetChanges[1].Actor)
			_, decisions, err = CheckLLMBudget(ctx, b, "fresh-workflow", now.AddDate(0, 2, 0))
			require.NoError(t, err)
			require.Empty(t, decisions)
			var wg sync.WaitGroup
			var successes atomic.Int32
			for i := 0; i < 4; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, e := b.SaveBudgetPolicy(ctx, p, "concurrent")
					if e == nil {
						successes.Add(1)
					}
				}()
			}
			wg.Wait()
			require.Equal(t, int32(1), successes.Load())
		})
	}
}
