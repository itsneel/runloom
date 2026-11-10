package store

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestEditLocksAcrossDatabaseHandles(t *testing.T) {
	ctx := context.Background()
	for _, driver := range []string{"postgres", "mongodb"} {
		t.Run(driver, func(t *testing.T) {
			var first, second Backend
			var err error
			if driver == "postgres" {
				url := os.Getenv("RUNLOOM_TEST_POSTGRES_URL")
				if url == "" {
					t.Skip("test database not configured")
				}
				schema := "runloom_lock_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
				a, e := NewPostgresTeam(ctx, url, schema)
				require.NoError(t, e)
				t.Cleanup(func() {
					_, e := a.db.ExecContext(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
					require.NoError(t, e)
					require.NoError(t, a.Close(ctx))
				})
				first = a
				second, err = NewPostgresTeam(ctx, url, schema)
			} else {
				url := os.Getenv("RUNLOOM_TEST_MONGODB_URL")
				if url == "" {
					t.Skip("test database not configured")
				}
				name := "runloom_lock_test_" + uuid.NewString()
				a, e := NewMongo(ctx, url, name)
				require.NoError(t, e)
				t.Cleanup(func() { require.NoError(t, a.database.Drop(ctx)); require.NoError(t, a.Close(ctx)) })
				first = a
				second, err = NewMongo(ctx, url, name)
			}
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, second.Close(ctx)) })
			tokens := []string{uuid.NewString(), uuid.NewString()}
			stores := []Backend{first, second}
			type result struct {
				index int
				err   error
			}
			results := make(chan result, 2)
			start := make(chan struct{})
			for i, b := range stores {
				go func(i int, b Backend) {
					<-start
					_, err := b.ChangeEditLock(ctx, "shared", "alice", "Alice", tokens[i], "acquire")
					results <- result{i, err}
				}(i, b)
			}
			close(start)
			winner := -1
			for i := 0; i < 2; i++ {
				r := <-results
				if r.err == nil {
					require.Equal(t, -1, winner)
					winner = r.index
				} else {
					require.ErrorIs(t, r.err, ErrEditLocked)
				}
			}
			require.NotEqual(t, -1, winner)
			other := stores[1-winner]
			_, err = other.BeginDefinitionWrite(ctx, "shared", "alice", "")
			require.ErrorIs(t, err, ErrEditLocked)
			_, err = other.ChangeEditLock(ctx, "shared", "alice", "Alice", tokens[winner], "renew")
			require.NoError(t, err)
			_, err = other.ChangeEditLock(ctx, "shared", "alice", "Alice", tokens[winner], "release")
			require.NoError(t, err)
			status, err := stores[winner].EditLock(ctx, "shared")
			require.NoError(t, err)
			require.False(t, status.Locked)
		})
	}
}
