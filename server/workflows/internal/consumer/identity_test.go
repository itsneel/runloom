package consumer

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMachineTokenBoundaries(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test", Algorithm: "RS256", Use: "sig"}}})
	}))
	defer server.Close()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "test"))
	require.NoError(t, err)
	verifier := NewVerifier(context.Background(), server.URL, server.URL, "runloom-execution")
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		valid  bool
	}{
		{"machine without email", func(m map[string]any) {}, true},
		{"Studio audience", func(m map[string]any) { m["aud"] = "runloom" }, false},
		{"human", func(m map[string]any) { delete(m, "runloom_identity") }, false},
		{"missing generation", func(m map[string]any) { delete(m, "runloom_generation") }, false},
		{"wrong issuer", func(m map[string]any) { m["iss"] = "https://invalid.example" }, false},
		{"expired", func(m map[string]any) { m["exp"] = time.Now().Add(-time.Hour).Unix() }, false},
		{"ID token", func(m map[string]any) { m["typ"] = "ID" }, false},
		{"missing subject", func(m map[string]any) { delete(m, "sub") }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := map[string]any{"iss": server.URL, "aud": "runloom-execution", "sub": "service-subject", "azp": "service-client", "typ": "Bearer", "exp": time.Now().Add(time.Minute).Unix(), "runloom_identity": "service_account", "runloom_generation": "generation", "scope": "runs:start runs:read"}
			tc.change(claims)
			raw, _ := json.Marshal(claims)
			signed, err := signer.Sign(raw)
			require.NoError(t, err)
			token, err := signed.CompactSerialize()
			require.NoError(t, err)
			identity, err := verifier.Verify(context.Background(), token)
			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, []string{"runs:start", "runs:read"}, identity.Scopes)
			} else {
				require.Error(t, err)
			}
		})
	}
}
