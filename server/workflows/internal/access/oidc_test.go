package access

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOIDCRejectsInvalidClaims(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	jwk := map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "test-key", "use": "sig", "alg": "RS256", "n": enc(key.N.Bytes()), "e": enc(big.NewInt(int64(key.E)).Bytes())}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(jwk) }))
	defer server.Close()
	verifier := NewOIDC(context.Background(), Config{Issuer: server.URL, JWKSURL: server.URL, ClientID: "runloom"})
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		valid  bool
	}{
		{"valid", func(c map[string]any) {}, true},
		{"wrong issuer", func(c map[string]any) { c["iss"] = "https://untrusted.example" }, false},
		{"wrong audience", func(c map[string]any) { c["aud"] = "another-app" }, false},
		{"expired", func(c map[string]any) { c["exp"] = time.Now().Add(-time.Hour).Unix() }, false},
		{"unverified email", func(c map[string]any) { c["email_verified"] = false }, false},
		{"ID token", func(c map[string]any) { c["typ"] = "ID" }, false},
		{"wrong client", func(c map[string]any) { c["azp"] = "another-app" }, false},
		{"missing subject", func(c map[string]any) { delete(c, "sub") }, false},
		{"missing email", func(c map[string]any) { delete(c, "email") }, false},
		{"not yet valid", func(c map[string]any) { c["nbf"] = time.Now().Add(time.Hour).Unix() }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := map[string]any{"iss": server.URL, "aud": "runloom", "sub": "user-one", "exp": time.Now().Add(time.Minute).Unix(), "iat": time.Now().Unix(), "email": "person@example.com", "email_verified": true, "name": "Person", "azp": "runloom", "typ": "Bearer"}
			tc.change(claims)
			head, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "test-key"})
			body, _ := json.Marshal(claims)
			payload := enc(head) + "." + enc(body)
			digest := sha256.Sum256([]byte(payload))
			sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
			if err != nil {
				t.Fatal(err)
			}
			u, err := verifier.Verify(context.Background(), payload+"."+enc(sig))
			if tc.valid {
				if err != nil || u.ID != "user-one" {
					t.Fatal("valid access token rejected", err)
				}
			} else if err == nil {
				t.Fatal("invalid claims accepted")
			}
		})
	}
	if _, err := verifier.Verify(context.Background(), "unsigned"); err == nil {
		t.Fatal("unsigned token accepted")
	}
}
