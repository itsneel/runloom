package access

import (
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

func TestPlatformAuthenticationFailsClosedWithoutMFA(t *testing.T) {
	t.Setenv("RUNLOOM_PLATFORM_ALLOW_PASSWORD_ONLY", "")
	t.Setenv("RUNLOOM_PLATFORM_REQUIRED_ACR", "")
	verifier := NewPlatformOIDC(t.Context(), Config{})
	if _, err := verifier.Verify(t.Context(), "token"); err == nil {
		t.Fatal("unconfigured hosted platform authentication must reject access")
	}
	t.Setenv("RUNLOOM_PLATFORM_REQUIRED_ACR", "mfa")
	t.Setenv("RUNLOOM_PLATFORM_MAX_AUTH_AGE", "invalid")
	verifier = NewPlatformOIDC(t.Context(), Config{})
	if _, err := verifier.Verify(t.Context(), "token"); err == nil {
		t.Fatal("invalid authentication age must reject access")
	}
}

func TestPlatformRequiresSignedMFAAndRecentAuthentication(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	jwk := map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "platform-test", "use": "sig", "alg": "RS256", "n": enc(key.N.Bytes()), "e": enc(big.NewInt(int64(key.E)).Bytes())}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(jwk) }))
	defer server.Close()
	t.Setenv("RUNLOOM_PLATFORM_ALLOW_PASSWORD_ONLY", "false")
	t.Setenv("RUNLOOM_PLATFORM_REQUIRED_ACR", "mfa")
	t.Setenv("RUNLOOM_PLATFORM_MAX_AUTH_AGE", "15m")
	verifier := NewPlatformOIDC(t.Context(), Config{Issuer: server.URL, JWKSURL: server.URL, ClientID: "runloom"})
	for _, tc := range []struct {
		name, acr string
		age       time.Duration
		valid     bool
	}{
		{"recent MFA", "mfa", time.Minute, true}, {"password only", "0", time.Minute, false},
		{"missing context", "", time.Minute, false}, {"old MFA", "mfa", time.Hour, false}, {"future authentication", "mfa", -time.Hour, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := map[string]any{"iss": server.URL, "aud": "runloom", "sub": "platform-user", "azp": "runloom", "typ": "Bearer", "exp": time.Now().Add(time.Minute).Unix(), "iat": time.Now().Unix(), "email": "admin@example.test", "email_verified": true, "acr": tc.acr, "auth_time": time.Now().Add(-tc.age).Unix()}
			header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "platform-test"})
			body, _ := json.Marshal(claims)
			payload := enc(header) + "." + enc(body)
			digest := sha256.Sum256([]byte(payload))
			signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
			if err != nil {
				t.Fatal(err)
			}
			_, err = verifier.Verify(t.Context(), payload+"."+enc(signature))
			if (err == nil) != tc.valid {
				t.Fatalf("unexpected authentication result: %v", err)
			}
		})
	}
}
