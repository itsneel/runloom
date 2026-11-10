package store

import (
	"github.com/example/runloom/workflows/internal/prompts"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
)

func exercisePromptStore(t *testing.T, b Backend) {
	t.Helper()
	ctx := t.Context()
	e := prompts.Entry{ID: "prompt-" + uuid.NewString(), Name: "Summary", Template: prompts.Template{Text: "Summarize ${vars.text}", Variables: []prompts.Variable{{Name: "text", Type: "string", Required: true}}}}
	first, err := b.SavePrompt(ctx, e, true, "trusted-actor")
	require.NoError(t, err)
	require.Equal(t, 1, first.Version)
	require.True(t, first.Published)
	_, err = b.SavePrompt(ctx, e, true, "stale")
	require.ErrorIs(t, err, ErrPromptConflict)
	next := first
	next.Tags = []string{" Support ", "billing", "support"}
	next.Template.Text = "Explain ${vars.text}"
	next.Actor = "forged"
	next.Version = 99
	next, err = b.SavePrompt(ctx, next, false, "editor")
	require.NoError(t, err)
	require.Equal(t, 1, next.Version)
	require.False(t, next.Published)
	require.Equal(t, "editor", next.Actor)
	require.Equal(t, []string{"support", "billing"}, next.Tags)
	pinned, err := b.GetPrompt(ctx, e.ID, 1)
	require.NoError(t, err)
	require.Equal(t, first, pinned)
	next, err = b.SavePrompt(ctx, next, true, "publisher")
	require.NoError(t, err)
	require.Equal(t, 2, next.Version)
	history, err := b.PromptHistory(ctx, e.ID)
	require.NoError(t, err)
	require.Len(t, history, 3)
	require.Equal(t, int64(3), history[0].Revision)
	_, err = b.GetPrompt(ctx, e.ID, 3)
	require.ErrorIs(t, err, ErrNotFound)
}
func TestFilePromptStore(t *testing.T) {
	path := t.TempDir()
	b, err := New(path)
	require.NoError(t, err)
	exercisePromptStore(t, b)
	reopened, err := New(path)
	require.NoError(t, err)
	list, err := reopened.ListPrompts(t.Context())
	require.NoError(t, err)
	require.Len(t, list, 1)
	other, err := New(t.TempDir())
	require.NoError(t, err)
	_, err = other.GetPrompt(t.Context(), list[0].ID, 1)
	require.ErrorIs(t, err, ErrNotFound)
}
