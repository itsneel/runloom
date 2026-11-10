package consumer

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"os"
	"sync"
	"testing"
)

func TestRepositoryCAS(t *testing.T) {
	for _, driver := range []string{"file", "postgres", "mongodb"} {
		t.Run(driver, func(t *testing.T) {
			connection := ""
			if driver != "file" {
				connection = os.Getenv("RUNLOOM_TEST_" + map[string]string{"postgres": "POSTGRES", "mongodb": "MONGODB"}[driver] + "_URL")
				if connection == "" {
					t.Skip("database integration not configured")
				}
			}
			ctx := context.Background()
			dir := t.TempDir()
			database := "runloom_consumer_tests"
			r, err := Open(ctx, driver, connection, database, dir)
			require.NoError(t, err)
			defer r.Close(ctx)
			other, err := Open(ctx, driver, connection, database, dir)
			require.NoError(t, err)
			defer other.Close(ctx)
			key := uuid.NewString()
			var value map[string]int
			_, err = r.Read(ctx, key, &value)
			require.ErrorIs(t, err, ErrNotFound)
			ok, err := r.Swap(ctx, key, -1, map[string]int{"count": 0})
			require.NoError(t, err)
			require.True(t, ok)
			ok, err = other.Swap(ctx, key, -1, map[string]int{"count": 999})
			require.NoError(t, err)
			require.False(t, ok)
			var wg sync.WaitGroup
			for i := 0; i < 12; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					repo := r
					if i%2 == 0 {
						repo = other
					}
					for {
						var v map[string]int
						rev, err := repo.Read(ctx, key, &v)
						if err != nil {
							t.Error(err)
							return
						}
						v["count"]++
						ok, err := repo.Swap(ctx, key, rev, v)
						if err != nil {
							t.Error(err)
							return
						}
						if ok {
							return
						}
					}
				}(i)
			}
			wg.Wait()
			_, err = other.Read(ctx, key, &value)
			require.NoError(t, err)
			require.Equal(t, 12, value["count"])
			_, err = r.Swap(ctx, "../escape", -1, value)
			require.Error(t, err)
		})
	}
}
