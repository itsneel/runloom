package access

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOneOrganizationFamily(t *testing.T) {
	ctx := t.Context()
	repo, err := Open(ctx, "file", "", "", t.TempDir())
	require.NoError(t, err)
	defer repo.Close(ctx)
	d := &Directory{Repo: repo}
	owner := User{ID: "owner", Email: "owner@example.test"}
	other := User{ID: "other", Email: "other@example.test"}
	root, err := d.CreateOrg(ctx, owner, "Family A")
	require.NoError(t, err)
	foreign, err := d.CreateOrg(ctx, other, "Family B")
	require.NoError(t, err)
	_, err = d.CreateOrg(ctx, owner, "Second root")
	require.ErrorContains(t, err, "one organization family")
	child, err := d.CreateOrganization(ctx, owner, "Child", root)
	require.NoError(t, err)
	require.ErrorContains(t, d.SetMember(ctx, other, foreign, "", owner.Email, "member", false), "one organization family")
	pending := User{ID: "pending", Email: "pending@example.test"}
	require.NoError(t, d.SetMember(ctx, owner, child, "", pending.Email, "member", false))
	require.ErrorContains(t, d.SetMember(ctx, other, foreign, "", pending.Email, "member", false), "one organization family")
	_, err = d.CreateOrg(ctx, pending, "Invitation bypass")
	require.ErrorContains(t, err, "one organization family")
	state, err := d.Bind(ctx, pending)
	require.NoError(t, err)
	require.Equal(t, "member", OrgRole(state, child, pending.ID))
	require.Empty(t, OrgRole(state, root, pending.ID), "child membership grants no parent access")
	require.False(t, CanCreateRootOrganization(state, pending))
	require.False(t, CanCreateRootOrganization(state, owner))
	require.NoError(t, d.SetMember(ctx, owner, root, "", pending.Email, "member", false))
	state, err = d.Bind(ctx, pending)
	require.NoError(t, err)
	require.Equal(t, "member", OrgRole(state, root, pending.ID))
	require.NoError(t, d.SetMember(ctx, owner, root, "", pending.Email, "", true))
	require.ErrorContains(t, d.SetMember(ctx, other, foreign, "", pending.Email, "member", false), "one organization family")
	require.NoError(t, d.SetMember(ctx, owner, child, "", pending.Email, "", true))
	require.NoError(t, d.SetMember(ctx, other, foreign, "", pending.Email, "member", false))
}

func TestConcurrentFirstOrganizationsShareOneFamily(t *testing.T) {
	repo, err := Open(t.Context(), "file", "", "", t.TempDir())
	require.NoError(t, err)
	defer repo.Close(t.Context())
	d := &Directory{Repo: repo}
	owner := User{ID: "owner", Email: "owner@example.test"}
	var results [2]error
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) { defer wg.Done(); _, results[i] = d.CreateOrg(t.Context(), owner, "Root") }(i)
	}
	wg.Wait()
	count := 0
	for _, err := range results {
		if err == nil {
			count++
		} else {
			require.ErrorContains(t, err, "one organization family")
		}
	}
	require.Equal(t, 1, count)
}

func TestFamilyValidationCoversSubjectChangesAndAllowsRepair(t *testing.T) {
	previous := NewState()
	previous.Orgs["a"] = Organization{ID: "a", Members: map[string]Member{"old@example.test": {Email: "old@example.test", UserID: "same", Role: "owner"}}}
	previous.Orgs["b"] = Organization{ID: "b", Members: map[string]Member{"new@example.test": {Email: "new@example.test", Role: "member"}}}
	next := NewState()
	for id, org := range previous.Orgs {
		next.Orgs[id] = org
	}
	next.Orgs["b"] = Organization{ID: "b", Members: map[string]Member{"new@example.test": {Email: "new@example.test", UserID: "same", Role: "member"}}}
	require.ErrorContains(t, validateOrganizationMembershipChanges(previous, next), "one organization family", "verified subject cannot use a changed email to join another family")
	repaired := NewState()
	repaired.Orgs["a"] = next.Orgs["a"]
	repaired.Orgs["b"] = Organization{ID: "b", Members: map[string]Member{}}
	require.NoError(t, validateOrganizationMembershipChanges(next, repaired))
}
