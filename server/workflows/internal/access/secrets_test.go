package access

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecretVaultStartupRequiresAnExactReadableKey(t *testing.T) {
	root := t.TempDir()
	keyFile := filepath.Join(root, "key")
	_, err := NewSecretVault(nil, keyFile)
	require.Error(t, err)
	for _, size := range []int{0, 31, 33, 44} {
		require.NoError(t, os.WriteFile(keyFile, make([]byte, size), 0600))
		_, err = NewSecretVault(nil, keyFile)
		require.Error(t, err)
	}
	require.NoError(t, os.WriteFile(keyFile, make([]byte, 32), 0600))
	vault, err := NewSecretVault(nil, keyFile)
	require.NoError(t, err)
	require.NotNil(t, vault)
}

func TestSecretVaultLegacyEncryptionAndIdentityBinding(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	repo, err := Open(ctx, "file", "", "", root)
	require.NoError(t, err)
	defer repo.Close(ctx)
	d := &Directory{Repo: repo}
	keyFile := filepath.Join(root, "key")
	require.NoError(t, os.WriteFile(keyFile, []byte(strings.Repeat("k", 32)), 0600))
	vault, err := NewSecretVault(d, keyFile)
	require.NoError(t, err)
	// Construct the old ciphertext format: origin remains authenticated metadata,
	// but callers no longer supply a destination to resolve the named value.
	nonce := make([]byte, vault.cipher.NonceSize())
	sealed := vault.cipher.Seal(nonce, nonce, []byte("synthetic-legacy-value"), []byte("team/key\x00https://old.example.com"))
	legacy := StoredSecret{Name: "key", TeamID: "team", LegacyOrigin: "https://old.example.com", Ciphertext: base64.StdEncoding.EncodeToString(sealed)}
	require.NoError(t, d.UpdateModule(ctx, "secrets", func(s *State) error {
		s.Secrets = map[string]StoredSecret{"team/key": legacy}
		return nil
	}))
	value, err := vault.Resolve(ctx, "team", "key")
	require.NoError(t, err)
	require.Equal(t, "synthetic-legacy-value", value)
	for _, target := range []struct{ team, name string }{{"other", "key"}, {"team", "renamed"}} {
		require.NoError(t, d.UpdateModule(ctx, "secrets", func(s *State) error {
			copy := legacy
			copy.TeamID, copy.Name = target.team, target.name
			s.Secrets[target.team+"/"+target.name] = copy
			return nil
		}))
		_, err = vault.Resolve(ctx, target.team, target.name)
		require.ErrorContains(t, err, "cannot be decrypted")
	}
	require.NoError(t, d.UpdateModule(ctx, "secrets", func(s *State) error {
		legacy.LegacyOrigin = "https://tampered.example.com"
		s.Secrets["team/key"] = legacy
		return nil
	}))
	_, err = vault.Resolve(ctx, "team", "key")
	require.ErrorContains(t, err, "cannot be decrypted")
}
