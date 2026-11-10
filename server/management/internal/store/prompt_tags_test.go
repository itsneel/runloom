package store

import (
	"context"
	"os"
	"testing"

	"github.com/example/runloom/management/internal/prompts"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestPromptTagsPersistence(t *testing.T) {
	for _, driver := range []string{"file", "postgres", "mongodb"} {
		t.Run(driver, func(t *testing.T) {
			ctx := t.Context()
			var b Backend
			var err error
			switch driver {
			case "file":
				b, err = New(t.TempDir())
			case "postgres":
				url := os.Getenv("RUNLOOM_TEST_POSTGRES_URL")
				if url == "" {
					t.Skip("set RUNLOOM_TEST_POSTGRES_URL to a disposable test database")
				}
				schema := "prompt_tags_" + uuid.NewString()[:8]
				var db *PostgresStore
				db, err = NewPostgresTeam(ctx, url, schema)
				if err == nil {
					t.Cleanup(func() {
						_, e := db.db.ExecContext(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
						require.NoError(t, e)
						require.NoError(t, db.Close(context.Background()))
					})
				}
				b = db
			case "mongodb":
				url := os.Getenv("RUNLOOM_TEST_MONGODB_URL")
				if url == "" {
					t.Skip("set RUNLOOM_TEST_MONGODB_URL to a disposable replica set")
				}
				var db *MongoStore
				db, err = NewMongo(ctx, url, "prompt_tags_"+uuid.NewString()[:8])
				if err == nil {
					t.Cleanup(func() {
						require.NoError(t, db.database.Drop(context.Background()))
						require.NoError(t, db.Close(context.Background()))
					})
				}
				b = db
			}
			require.NoError(t, err)
			entry := prompts.Entry{ID: "tagged-prompt", Name: "Tagged prompt", Tags: []string{" Support ", "BILLING", "support"}, Template: prompts.Template{Text: "Hello"}}
			first, err := b.SavePrompt(ctx, entry, true, "editor")
			require.NoError(t, err)
			require.Equal(t, []string{"support", "billing"}, first.Tags)
			draft := first
			draft.Tags = []string{"onboarding"}
			draft, err = b.SavePrompt(ctx, draft, false, "editor")
			require.NoError(t, err)
			require.Equal(t, first.Hash, draft.Hash, "Tags must not change template hashes")
			pinned, err := b.GetPrompt(ctx, entry.ID, 1)
			require.NoError(t, err)
			require.Equal(t, first.Tags, pinned.Tags, "Published tags are immutable")
			list, err := b.ListPrompts(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"onboarding"}, list[0].Tags)
			history, err := b.PromptHistory(ctx, entry.ID)
			require.NoError(t, err)
			require.Equal(t, first.Tags, history[1].Tags)
			require.Equal(t, draft.Tags, history[0].Tags)
			invalid := draft
			invalid.Tags = []string{"bad,tag"}
			_, err = b.SavePrompt(ctx, invalid, false, "editor")
			require.ErrorIs(t, err, ErrInvalidPrompt)
			current, err := b.GetPrompt(ctx, entry.ID, 0)
			require.NoError(t, err)
			require.Equal(t, draft.Revision, current.Revision, "Invalid tags must not write a revision")
			draft.Tags = []string{}
			cleared, err := b.SavePrompt(ctx, draft, false, "editor")
			require.NoError(t, err)
			require.Empty(t, cleared.Tags)
		})
	}
}
