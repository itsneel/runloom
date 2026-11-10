package store

import (
	"github.com/example/runloom/workflows/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
)

func exerciseInvalidDrafts(t *testing.T, b Backend) {
	t.Helper()
	d := model.Definition{SchemaVersion: "1.0", ID: "draft-" + uuid.NewString(), Name: "Work in progress", Steps: []model.Step{{ID: "done", Type: "end"}}}
	_, err := b.SaveDraft(d)
	require.NoError(t, err)
	first, err := b.Publish(d.ID)
	require.NoError(t, err)
	require.Equal(t, 1, first.Version)
	d.Steps = []model.Step{{ID: "ask", Type: "llm", Model: "default", Prompt: ""}}
	saved, commit, err := b.CommitDraft(d, CommitMeta{Actor: "editor"})
	require.NoError(t, err)
	require.Zero(t, saved.Version)
	require.Empty(t, saved.Steps[0].Prompt)
	_, err = b.Publish(d.ID)
	require.Error(t, err)
	latest, err := b.Get(d.ID, nil)
	require.NoError(t, err)
	require.Equal(t, first, latest)
	history, err := b.ListCommits(d.ID, 10)
	require.NoError(t, err)
	require.Len(t, history, 3, "failed publication must not create a commit")
	restored, _, err := b.RestoreCommit(d.ID, commit.CommitID, CommitMeta{Actor: "editor"})
	require.NoError(t, err)
	require.Empty(t, restored.Steps[0].Prompt)
	d.Steps = nil
	_, err = b.SaveDraft(d)
	require.NoError(t, err)
	_, err = b.Publish(d.ID)
	require.Error(t, err)
	d.Steps = []model.Step{{ID: "done", Type: "end"}}
	_, err = b.SaveDraft(d)
	require.NoError(t, err)
	published, err := b.Publish(d.ID)
	require.NoError(t, err)
	require.Equal(t, 2, published.Version)
	d.ID = "../unsafe"
	_, err = b.SaveDraft(d)
	require.Error(t, err)
}
func TestInvalidDraftPersistence(t *testing.T) {
	b, err := New(t.TempDir())
	require.NoError(t, err)
	exerciseInvalidDrafts(t, b)
}
