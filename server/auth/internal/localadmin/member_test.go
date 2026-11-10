package localadmin

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/example/runloom/auth/internal/access"
	"github.com/stretchr/testify/require"
)

func exerciseMember(t *testing.T, d *access.Directory) {
	t.Helper()
	ctx := context.Background()
	admin := access.User{ID: "bootstrap-admin", Email: Email}
	member := access.User{ID: "bootstrap-member", Email: MemberEmail}
	require.NoError(t, EnsureDirectory(ctx, d, admin))
	require.NoError(t, EnsureMemberDirectory(ctx, d, member))
	state, _, err := d.Repo.Read(ctx)
	require.NoError(t, err)
	require.Len(t, state.Orgs, 1)
	var orgID, teamID string
	for id, org := range state.Orgs {
		orgID = id
		require.Len(t, org.Teams, 1)
		for id := range org.Teams {
			teamID = id
		}
	}
	require.Equal(t, "owner", access.OrgRole(state, orgID, admin.ID))
	require.Equal(t, "member", access.OrgRole(state, orgID, member.ID))
	_, role := access.TeamRole(state, teamID, member.ID)
	require.Equal(t, "editor", role)
	_, role = access.TeamRole(state, teamID, admin.ID)
	require.Equal(t, "admin", role)
	require.ErrorIs(t, d.SetMember(ctx, member, orgID, teamID, member.Email, "admin", false), access.ErrForbidden)
	require.ErrorIs(t, d.SetMember(ctx, member, orgID, "", member.Email, "owner", false), access.ErrForbidden)
	_, err = d.CreateOrg(ctx, member, "Member must not create this")
	require.ErrorIs(t, err, access.ErrForbidden)

	before, err := json.Marshal(state)
	require.NoError(t, err)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			require.NoError(t, EnsureMemberDirectory(ctx, d, member))
		}()
	}
	wg.Wait()
	state, _, err = d.Repo.Read(ctx)
	require.NoError(t, err)
	after, err := json.Marshal(state)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after))
	require.NotContains(t, string(after), MemberPassword)
	events := 0
	for _, event := range state.Events {
		if event.Action == "local.member.ensured" {
			events++
			require.Equal(t, MemberEmail, event.Target)
		}
	}
	require.Equal(t, 1, events)

	require.NoError(t, d.SetMember(ctx, admin, orgID, teamID, member.Email, "viewer", false))
	require.NoError(t, EnsureMemberDirectory(ctx, d, member))
	state, _, err = d.Repo.Read(ctx)
	require.NoError(t, err)
	_, role = access.TeamRole(state, teamID, member.ID)
	require.Equal(t, "viewer", role, "re-running setup must preserve deliberate permission changes")
	require.ErrorContains(t, EnsureMemberDirectory(ctx, d, access.User{ID: "other-subject", Email: MemberEmail}), "different identity")
	require.Error(t, EnsureMemberDirectory(ctx, d, admin))
}

func TestMemberDirectory(t *testing.T) {
	exerciseMember(t, testDirectory(t))
}

func TestMemberDatabaseDirectory(t *testing.T) {
	for _, tc := range []struct{ driver, variable string }{
		{"postgres", "RUNLOOM_TEST_POSTGRES_URL"},
		{"mongodb", "RUNLOOM_TEST_MONGODB_URL"},
	} {
		t.Run(tc.driver, func(t *testing.T) {
			uri := os.Getenv(tc.variable)
			if uri == "" {
				t.Skip("disposable database URL not configured")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			repo, err := access.Open(ctx, tc.driver, uri, "runloom_local_member_test", t.TempDir())
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, repo.Close(context.Background())) })
			exerciseMember(t, &access.Directory{Repo: repo})
		})
	}
}
