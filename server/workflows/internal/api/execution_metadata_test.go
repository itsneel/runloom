package api

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/stretchr/testify/require"
)

func TestExecutionMetadataPinnedFromCatalog(t *testing.T) {
	b, err := store.New(t.TempDir())
	require.NoError(t, err)
	s := New(b, nil, "test")
	defer s.Close()
	s.orgID, s.namespace = "trusted-org", "trusted-namespace"
	d := model.Definition{SchemaVersion: "1.0", ID: "example", Name: "Example", Kind: "main", Trigger: model.Trigger{Type: "manual"}, Steps: []model.Step{{ID: "echo", Type: "activity", Activity: "runloom.echo"}}}
	d, err = b.SaveDraft(d)
	require.NoError(t, err)
	p, err := b.CreateProject(context.Background(), "Private project")
	require.NoError(t, err)
	require.NoError(t, b.AssignProject(context.Background(), d.ID, p.ID))
	r := httptest.NewRequest("POST", "/api/v1/studio/runs", nil)
	r.Header.Set("X-Runloom-Team", "trusted-team")
	r.Header.Set("X-Runloom-Namespace", "forged")
	a, err := s.prepareRun(r, d, map[string]any{"RunloomOrgId": "forged"})
	require.NoError(t, err)
	second, err := s.prepareRun(r, d, map[string]any{})
	require.NoError(t, err)
	require.Equal(t, a.Metadata.SnapshotID, second.Metadata.SnapshotID)
	require.Equal(t, "trusted-org", a.Metadata.OrgID)
	require.Equal(t, "trusted-namespace", a.Metadata.Namespace)
	require.Equal(t, p.ID, a.Metadata.ProjectID)
	tagRun(&a, "execution", "draft_test", true)
	require.Equal(t, "execution", a.Metadata.RootExecutionID)
	require.True(t, a.Metadata.IsTest)
	d.Name = "Changed snapshot"
	changed, err := s.prepareRun(r, d, nil)
	require.NoError(t, err)
	require.NotEqual(t, a.Metadata.SnapshotID, changed.Metadata.SnapshotID)
	_, err = b.RecordRunStart(store.RunRecord{WorkflowID: "execution", Metadata: a.Metadata})
	require.NoError(t, err)
	saved, err := b.GetRun("execution")
	require.NoError(t, err)
	require.Equal(t, a.Metadata, saved.Metadata)
}
