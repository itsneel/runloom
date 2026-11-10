package tests

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"github.com/example/runloom/assistant/internal/chat"
	runloomauth "github.com/example/runloom/sdk/go-auth"
	obs "github.com/example/runloom/sdk/go-observability"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestVerifiedIdentityAndLiveMembership(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	var allowed atomic.Bool
	allowed.Store(true)
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/jwks" {
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "test", "use": "sig", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": "AQAB"}}})
			return
		}
		if r.URL.Path == "/api/v1/studio/me" {
			if len(r.Header.Get(obs.RequestIDHeader)) != 32 {
				t.Error("membership request missing correlation ID")
			}
			teams := []map[string]string{}
			if allowed.Load() {
				teams = append(teams, map[string]string{"id": "team", "role": "editor"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"user": map[string]string{"id": "alice"}, "organizations": []any{map[string]any{"id": "org", "teams": teams}}})
			return
		}
		http.NotFound(w, r)
	}))
	defer issuer.Close()
	verifier := runloomauth.New(context.Background(), runloomauth.Config{Issuer: issuer.URL, JWKSURL: issuer.URL + "/jwks", Audience: "runloom", ClientID: "runloom", Kind: runloomauth.Human})
	authorize := chat.Authorization(verifier, issuer.URL)
	token := func(overrides map[string]any) string {
		claims := map[string]any{"iss": issuer.URL, "sub": "alice", "aud": "runloom", "azp": "runloom", "typ": "Bearer", "exp": time.Now().Add(time.Hour).Unix(), "email": "alice@example.test", "email_verified": true}
		for k, v := range overrides {
			claims[k] = v
		}
		data, _ := json.Marshal(claims)
		header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "test"})
		signed := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(data)
		hash := sha256.Sum256([]byte(signed))
		signature, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
		return signed + "." + base64.RawURLEncoding.EncodeToString(signature)
	}
	r := httptest.NewRequest("GET", "/test", nil)
	r.Header.Set("Authorization", "Bearer "+token(nil))
	scope, err := authorize(context.Background(), r, "org", "team")
	if err != nil || scope.User != "alice" {
		t.Fatal("human authorization failed")
	}
	telemetry, err := obs.Start(context.Background(), "assistant", "")
	if err != nil {
		t.Fatal(err)
	}
	defer telemetry.Close()
	telemetry.Wrap(http.HandlerFunc(func(w http.ResponseWriter, inbound *http.Request) {
		if _, err := authorize(inbound.Context(), inbound, "org", "team"); err != nil {
			t.Error(err)
		}
	})).ServeHTTP(httptest.NewRecorder(), r)
	allowed.Store(false)
	if _, err = authorize(context.Background(), r, "org", "team"); err != chat.ErrForbidden {
		t.Fatal("membership revocation ignored")
	}
	allowed.Store(true)
	for name, claims := range map[string]map[string]any{"consumer": {"runloom_identity": "service_account"}, "unverified": {"email_verified": false}, "audience": {"aud": "execution"}, "expired": {"exp": time.Now().Add(-time.Hour).Unix()}, "spoofed": {"sub": "bob"}} {
		t.Run(name, func(t *testing.T) {
			r.Header.Set("Authorization", "Bearer "+token(claims))
			r.Header.Set("X-Runloom-Actor", "alice")
			if _, err = authorize(context.Background(), r, "org", "team"); err != chat.ErrUnauthorized {
				t.Fatal("invalid identity accepted")
			}
		})
	}
}
