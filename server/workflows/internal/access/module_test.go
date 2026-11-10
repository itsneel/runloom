package access

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestModuleOwnershipAndComposedAuditOrder(t *testing.T) {
	ctx := context.Background()
	r, err := Open(ctx, "file", "", "", t.TempDir())
	require.NoError(t, err)
	defer r.Close(ctx)
	d := &Directory{Repo: r}
	require.NoError(t, d.Update(ctx, func(s *State) error {
		s.Users["owner"] = User{ID: "owner"}
		s.Events = append(s.Events, Event{Action: "identity", CreatedAt: "2026-10-02T12:00:00Z"})
		return nil
	}))
	_, revision, err := r.ReadOwned(ctx)
	require.NoError(t, err)
	require.NoError(t, d.UpdateModule(ctx, "secrets", func(s *State) error {
		require.Contains(t, s.Users, "owner", "authorization projection is available")
		s.Secrets = map[string]StoredSecret{"team/key": {Name: "key", TeamID: "team", Ciphertext: "synthetic-ciphertext"}}
		s.Events = append(s.Events, Event{Action: "secret", CreatedAt: "2026-10-02T11:00:00Z"})
		return nil
	}))
	owned, current, err := r.ReadOwned(ctx)
	require.NoError(t, err)
	require.Equal(t, revision, current, "secrets do not contend with identity revision")
	require.Empty(t, owned.Secrets)
	secret, _, err := r.Module("secrets").ReadOwned(ctx)
	require.NoError(t, err)
	require.Empty(t, secret.Users)
	require.Empty(t, secret.Orgs)
	combined, _, err := r.Read(ctx)
	require.NoError(t, err)
	require.Contains(t, combined.Secrets, "team/key")
	require.Contains(t, combined.Users, "owner")
	require.Equal(t, []string{"secret", "identity"}, []string{combined.Events[0].Action, combined.Events[1].Action})
}
