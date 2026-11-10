package access

import (
	"context"
	"github.com/example/runloom/workflows/internal/dbnode"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type connectionConflictRepo struct {
	Repository
	conflict *bool
	scope    string
}

func (r *connectionConflictRepo) Module(scope string) Repository {
	return &connectionConflictRepo{Repository: r.Repository.Module(scope), conflict: r.conflict, scope: scope}
}
func (r *connectionConflictRepo) Swap(ctx context.Context, rev int64, s State) (bool, error) {
	if r.scope == "secrets" && *r.conflict {
		*r.conflict = false
		return false, nil
	}
	return r.Repository.Swap(ctx, rev, s)
}

func TestConnectionCredentialSurvivesOptimisticRetry(t *testing.T) {
	ctx := context.Background()
	r, err := Open(ctx, "file", "", "", t.TempDir())
	require.NoError(t, err)
	defer r.Close(ctx)
	conflict := false
	d := &Directory{Repo: &connectionConflictRepo{Repository: r, conflict: &conflict}}
	u := User{ID: "owner", Email: "owner@example.test"}
	_, err = d.Bind(ctx, u)
	require.NoError(t, err)
	org, err := d.CreateOrg(ctx, u, "Connections")
	require.NoError(t, err)
	s, _, err := d.Repo.Read(ctx)
	require.NoError(t, err)
	team := ""
	for id := range s.Orgs[org].Teams {
		team = id
	}
	key := filepath.Join(t.TempDir(), "key")
	require.NoError(t, os.WriteFile(key, []byte(strings.Repeat("k", 32)), 0600))
	v, err := NewSecretVault(d, key)
	require.NoError(t, err)
	c := dbnode.Connection{ID: "reporting", Name: "Reporting", Provider: "postgres", Host: "db.example.test", Port: 5432, Database: "reporting", Username: "reader", Password: "synthetic-first", Access: "read", TLS: true, Enabled: true}
	for _, password := range []string{"synthetic-first", "synthetic-rotated"} {
		c.Password = password
		conflict = true
		require.NoError(t, v.PutConnection(ctx, u, org, team, c))
		saved, err := v.ResolveConnection(ctx, team, c.ID)
		require.NoError(t, err)
		require.True(t, saved.Password == password)
	}
	// Ciphertext cannot be copied to another ID, even inside the same team.
	require.NoError(t, d.UpdateModule(ctx, "secrets", func(s *State) error {
		copy := s.Connections[team+"/"+c.ID]
		copy.Connection.ID = "copied"
		s.Connections[team+"/copied"] = copy
		return nil
	}))
	_, err = v.ResolveConnection(ctx, team, "copied")
	require.ErrorContains(t, err, "cannot be decrypted")
}
