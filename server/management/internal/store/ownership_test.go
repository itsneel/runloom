package store

import (
	"context"
	"github.com/example/runloom/management/internal/budget"
	"github.com/example/runloom/management/internal/model"
	"github.com/example/runloom/management/internal/prompts"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestManagementOwnedPromptAndPolicyRevisions(t *testing.T) {
	ctx := t.Context()
	b, err := New(t.TempDir())
	require.NoError(t, err)
	b.ConfigurePolicyReferences(func(string, *int) (model.Definition, error) { return model.Definition{}, ErrNotFound }, func(context.Context) (ProjectCatalog, error) {
		return ProjectCatalog{Projects: map[string]Project{"default": {ID: "default", Name: "General"}}}, nil
	})
	p, err := b.SavePrompt(ctx, prompts.Entry{ID: "welcome", Name: "Welcome", Template: prompts.Template{Text: "Hello"}}, true, "author")
	require.NoError(t, err)
	require.Equal(t, 1, p.Version)
	_, err = b.SavePrompt(ctx, prompts.Entry{ID: "welcome", Name: "Welcome", Template: prompts.Template{Text: "Changed"}}, false, "other")
	require.ErrorIs(t, err, ErrPromptConflict)
	policy := budget.Policy{ID: uuid.NewString(), Name: "Monthly", Enabled: true, SoftUSD: "1", HardUSD: "2", Projects: []string{"default"}, Reason: "test"}
	saved, err := b.SaveBudgetPolicy(ctx, policy, "owner")
	require.NoError(t, err)
	require.Equal(t, int64(1), saved.Revision)
	_, err = b.SaveBudgetPolicy(ctx, policy, "stale")
	require.ErrorIs(t, err, ErrBudgetConflict)
	catalog, err := b.ProjectCatalog(ctx)
	require.NoError(t, err)
	require.Empty(t, catalog.Projects, "project records belong to Workflows, not this store")
	require.Len(t, catalog.BudgetPolicies, 1)
	require.Len(t, catalog.BudgetChanges, 1)
	require.Equal(t, "owner", catalog.BudgetChanges[0].Actor)
}
