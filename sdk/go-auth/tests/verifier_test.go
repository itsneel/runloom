package tests

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	auth "github.com/example/runloom/sdk/go-auth"
	jose "github.com/go-jose/go-jose/v4"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIdentityBoundaries(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test", Algorithm: "RS256", Use: "sig"}}})
	}))
	defer server.Close()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "test"))
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []auth.Kind{auth.Human, auth.ServiceAccount} {
		audience := "runloom"
		if kind == auth.ServiceAccount {
			audience = "runloom-execution"
		}
		verifier := auth.New(context.Background(), auth.Config{Issuer: server.URL, JWKSURL: server.URL, Audience: audience, ClientID: "runloom", Kind: kind, RequiredACR: "runloom-mfa", MaxAuthAge: 5 * time.Minute})
		cases := []struct {
			name   string
			change func(map[string]any)
			valid  bool
		}{
			{"valid", func(map[string]any) {}, true},
			{"wrong issuer", func(c map[string]any) { c["iss"] = "https://wrong.test" }, false},
			{"wrong audience", func(c map[string]any) { c["aud"] = "wrong" }, false},
			{"expired", func(c map[string]any) { c["exp"] = time.Now().Add(-time.Hour).Unix() }, false},
			{"future nbf", func(c map[string]any) { c["nbf"] = time.Now().Add(time.Hour).Unix() }, false},
			{"missing subject", func(c map[string]any) { delete(c, "sub") }, false},
			{"missing expiration", func(c map[string]any) { delete(c, "exp") }, false},
			{"ID token", func(c map[string]any) { c["typ"] = "ID" }, false},
			{"missing party", func(c map[string]any) { delete(c, "azp") }, false},
			{"wrong identity", func(c map[string]any) {
				if kind == auth.Human {
					c["runloom_identity"] = "service_account"
				} else {
					delete(c, "runloom_identity")
				}
			}, false},
		}
		if kind == auth.Human {
			for name, change := range map[string]func(map[string]any){
				"missing assurance":     func(c map[string]any) { delete(c, "acr") },
				"wrong assurance":       func(c map[string]any) { c["acr"] = "password" },
				"missing auth time":     func(c map[string]any) { delete(c, "auth_time") },
				"stale authentication":  func(c map[string]any) { c["auth_time"] = time.Now().Add(-time.Hour).Unix() },
				"future authentication": func(c map[string]any) { c["auth_time"] = time.Now().Add(time.Hour).Unix() },
			} {
				cases = append(cases, struct {
					name   string
					change func(map[string]any)
					valid  bool
				}{name, change, false})
			}
			cases = append(cases, struct {
				name   string
				change func(map[string]any)
				valid  bool
			}{"unverified", func(c map[string]any) { c["email_verified"] = false }, false})
		}
		if kind == auth.ServiceAccount {
			cases = append(cases, struct {
				name   string
				change func(map[string]any)
				valid  bool
			}{"no generation", func(c map[string]any) { delete(c, "runloom_generation") }, false})
		}
		for _, tc := range cases {
			t.Run(string(kind)+"/"+tc.name, func(t *testing.T) {
				claims := map[string]any{"iss": server.URL, "aud": audience, "sub": "subject", "azp": "runloom", "typ": "Bearer", "exp": time.Now().Add(time.Minute).Unix()}
				if kind == auth.Human {
					claims["acr"] = "runloom-mfa"
					claims["auth_time"] = time.Now().Unix()
					claims["email"] = " User@Example.test "
					claims["email_verified"] = true
				} else {
					claims["runloom_identity"] = "service_account"
					claims["runloom_generation"] = "g1"
					claims["scope"] = "runs:read runs:start"
				}
				tc.change(claims)
				raw, _ := json.Marshal(claims)
				signed, err := signer.Sign(raw)
				if err != nil {
					t.Fatal(err)
				}
				token, err := signed.CompactSerialize()
				if err != nil {
					t.Fatal(err)
				}
				identity, err := verifier.Verify(context.Background(), token)
				if (err == nil) != tc.valid {
					t.Fatalf("valid=%v error=%v", tc.valid, err)
				}
				if err != nil && !errors.Is(err, auth.ErrInvalidToken) {
					t.Fatal("unsafe error")
				}
				if tc.valid && kind == auth.Human && identity.Email != "user@example.test" {
					t.Fatal("email not normalized")
				}
			})
		}
	}
}
func TestBearer(t *testing.T) {
	for _, headers := range [][]string{nil, {"Basic abc"}, {"Bearer"}, {"Bearer one two"}, {"Bearer " + strings.Repeat("x", 16385)}, {"Bearer one", "Bearer two"}} {
		r := httptest.NewRequest("GET", "/", nil)
		for _, h := range headers {
			r.Header.Add("Authorization", h)
		}
		if _, err := auth.Bearer(r); err == nil {
			t.Fatal("accepted invalid header")
		}
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "bearer token")
	if token, err := auth.Bearer(r); err != nil || token != "token" {
		t.Fatal("valid bearer rejected")
	}
}
