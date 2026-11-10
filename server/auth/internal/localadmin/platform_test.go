package localadmin

import (
	"context"
	"sync"
	"testing"

	"github.com/example/runloom/auth/internal/access"
	"github.com/stretchr/testify/require"
)

func TestPlatformAccountsIdentityPreservation(t *testing.T) {
	for _, account := range []struct{ email, password string }{
		{PlatformAdminEmail, PlatformAdminPassword}, {PlatformStaffEmail, PlatformStaffPassword},
	} {
		t.Run(account.email, func(t *testing.T) {
			testIdentityCreatePreserveAndReset(t, account.email, account.password, func(ctx context.Context, base string, reset bool) (access.User, bool, error) {
				return ensureIdentity(ctx, base, reset, account.email, account.password, "Local Platform Account")
			})
		})
	}
}

func exercisePlatformAccounts(t *testing.T, d *access.Directory, other *access.Directory) {
	t.Helper()
	ctx := context.Background()
	admin := access.User{ID: "local-platform-admin", Email: PlatformAdminEmail}
	staff := access.User{ID: "local-platform-staff", Email: PlatformStaffEmail}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			directory := []*access.Directory{d, other}[i%2]
			for _, user := range []access.User{admin, staff} {
				require.NoError(t, EnsurePlatformDirectory(ctx, directory, user))
			}
		}()
	}
	wg.Wait()
	state, _, err := other.Repo.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, access.PlatformAdmin, state.PlatformRoles[admin.ID])
	require.Equal(t, access.PlatformStaff, state.PlatformRoles[staff.ID])
	require.Len(t, state.PlatformEvents, 2, "concurrent/repeated setup must not duplicate grants or audits")
	for id, org := range state.Orgs {
		for _, user := range []access.User{admin, staff} {
			require.Empty(t, access.OrgRole(state, id, user.ID))
			for team := range org.Teams {
				_, role := access.TeamRole(state, team, user.ID)
				require.Empty(t, role)
			}
		}
	}
	require.ErrorIs(t, d.SetPlatformRole(ctx, staff, staff.Email, access.PlatformAdmin, "Forbidden escalation"), access.ErrForbidden)
	require.NoError(t, d.SetPlatformRole(ctx, admin, staff.Email, "", "Revoke staff access"))
	require.NoError(t, EnsurePlatformDirectory(ctx, other, staff))
	state, _, err = other.Repo.Read(ctx)
	require.NoError(t, err)
	require.Empty(t, state.PlatformRoles[staff.ID], "setup must not undo a revocation")
	require.Len(t, state.PlatformEvents, 3)
	require.NoError(t, d.SetPlatformRole(ctx, admin, staff.Email, access.PlatformAdmin, "Test role preservation"))
	require.NoError(t, d.SetPlatformRole(ctx, admin, admin.Email, access.PlatformStaff, "Test administrator demotion"))
	require.NoError(t, EnsurePlatformDirectory(ctx, other, admin))
	require.NoError(t, EnsurePlatformDirectory(ctx, other, staff))
	state, _, err = other.Repo.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, access.PlatformStaff, state.PlatformRoles[admin.ID])
	require.Equal(t, access.PlatformAdmin, state.PlatformRoles[staff.ID])
	require.Error(t, EnsurePlatformDirectory(ctx, d, access.User{ID: "different-subject", Email: PlatformStaffEmail}))
	require.Error(t, EnsurePlatformDirectory(ctx, d, access.User{ID: admin.ID, Email: PlatformStaffEmail}))
	require.Error(t, EnsurePlatformDirectory(ctx, d, access.User{ID: "other", Email: Email}))
}

func TestPlatformAccountsDirectory(t *testing.T) {
	d := testDirectory(t)
	require.NoError(t, EnsureDirectory(context.Background(), d, access.User{ID: "org-admin", Email: Email}))
	state, _, err := d.Repo.Read(context.Background())
	require.NoError(t, err)
	require.Empty(t, state.PlatformRoles, "organization bootstrap must not grant platform access")
	exercisePlatformAccounts(t, d, d)
}

func TestPlatformSetupPreservesExistingGrantAndRevocation(t *testing.T) {
	for _, role := range []string{"", access.PlatformStaff, access.PlatformAdmin} {
		t.Run("existing_"+role, func(t *testing.T) {
			d := testDirectory(t)
			ctx := context.Background()
			user := access.User{ID: "existing", Email: PlatformAdminEmail}
			require.NoError(t, d.Update(ctx, func(s *access.State) error {
				s.Users[user.ID] = user
				s.PlatformRoles[user.ID] = role
				s.PlatformEvents = append(s.PlatformEvents, access.PlatformEvent{Target: user.ID, Action: "platform.role.changed", Role: role})
				return nil
			}))
			require.NoError(t, EnsurePlatformDirectory(ctx, d, user))
			state, _, err := d.Repo.Read(ctx)
			require.NoError(t, err)
			require.Equal(t, role, state.PlatformRoles[user.ID])
		})
	}
}

func exerciseOrganizationAdminSeparation(t *testing.T, d *access.Directory, user access.User) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, EnsureDirectory(ctx, d, user))
	require.NoError(t, d.Update(ctx, func(s *access.State) error {
		s.PlatformRoles[user.ID] = access.PlatformAdmin
		return nil
	}))
	before, _, err := d.Repo.Read(ctx)
	require.NoError(t, err)
	require.ErrorContains(t, SeparateOrganizationAdmin(ctx, d, user), "dedicated local platform administrator")
	unchanged, _, err := d.Repo.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, before, unchanged, "failed migration must not remove access")
	platform := access.User{ID: "local-platform-admin", Email: PlatformAdminEmail}
	require.NoError(t, EnsurePlatformDirectory(ctx, d, platform))
	before, _, err = d.Repo.Read(ctx)
	require.NoError(t, err)
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			require.NoError(t, SeparateOrganizationAdmin(ctx, d, user))
		}()
	}
	wg.Wait()
	require.NoError(t, EnsureDirectory(ctx, d, user))
	require.NoError(t, SeparateOrganizationAdmin(ctx, d, user))
	after, _, err := d.Repo.Read(ctx)
	require.NoError(t, err)
	require.Empty(t, after.PlatformRoles[user.ID])
	require.Equal(t, access.PlatformAdmin, after.PlatformRoles[platform.ID])
	require.Equal(t, before.Orgs, after.Orgs, "organization/team permissions must not change")
	require.Equal(t, before.Users, after.Users)
	require.Equal(t, before.Events, after.Events)
	require.Len(t, after.PlatformEvents, len(before.PlatformEvents)+1)
	event := after.PlatformEvents[len(after.PlatformEvents)-1]
	require.Equal(t, "operator:local-bootstrap", event.Actor)
	require.Equal(t, user.ID, event.Target)
	require.Equal(t, access.PlatformAdmin, event.PreviousRole)
	require.Empty(t, event.Role)
	require.Error(t, SeparateOrganizationAdmin(ctx, d, platform))
	require.Error(t, SeparateOrganizationAdmin(ctx, d, access.User{ID: "wrong", Email: Email}))
}

func TestOrganizationAdminSeparation(t *testing.T) {
	exerciseOrganizationAdminSeparation(t, testDirectory(t), access.User{ID: "org-admin", Email: Email})
}
