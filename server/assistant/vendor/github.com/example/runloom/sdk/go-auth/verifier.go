// Package runloomauth verifies Keycloak access tokens without accessing application databases.
package runloomauth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

var ErrInvalidToken = errors.New("invalid access token")

type Kind string

const (
	Human          Kind = "human"
	ServiceAccount Kind = "service_account"
)

type Config struct {
	Issuer   string
	JWKSURL  string
	Audience string
	// ClientID is the required authorized party for human Studio tokens.
	ClientID   string
	Kind       Kind
	HTTPClient *http.Client
	// RequiredACR must match a signed authentication-context claim configured in
	// Keycloak. An arbitrary claim string does not itself establish MFA.
	RequiredACR string
	MaxAuthAge  time.Duration
}

type Identity struct {
	Subject    string
	ClientID   string
	Email      string
	Name       string
	Generation string
	Scopes     []string
	Kind       Kind
}

type Verifier struct {
	config   Config
	verifier *oidc.IDTokenVerifier
}

// New creates a verifier with a cached remote key set. Key fetching is bounded;
// permission checks and credential revocation remain the caller's responsibility.
func New(ctx context.Context, c Config) *Verifier {
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	ctx = oidc.ClientContext(ctx, client)
	return &Verifier{config: c, verifier: oidc.NewVerifier(c.Issuer, oidc.NewRemoteKeySet(ctx, c.JWKSURL), &oidc.Config{ClientID: c.Audience, SupportedSigningAlgs: []string{"RS256"}})}
}

func (v *Verifier) Verify(ctx context.Context, raw string) (Identity, error) {
	if v.config.Issuer == "" || v.config.Audience == "" || v.config.JWKSURL == "" || len(raw) == 0 || len(raw) > 16384 {
		return Identity{}, ErrInvalidToken
	}
	token, err := v.verifier.Verify(ctx, raw)
	if err != nil {
		return Identity{}, ErrInvalidToken
	}
	var c struct {
		Client        string `json:"azp"`
		Type          string `json:"typ"`
		Kind          string `json:"runloom_identity"`
		Generation    string `json:"runloom_generation"`
		Scope         string `json:"scope"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		NotBefore     int64  `json:"nbf"`
		ACR           string `json:"acr"`
		AuthTime      int64  `json:"auth_time"`
	}
	if token.Claims(&c) != nil || token.Subject == "" || c.Type != "Bearer" || c.Client == "" || c.NotBefore > time.Now().Unix() {
		return Identity{}, ErrInvalidToken
	}
	switch v.config.Kind {
	case Human:
		if v.config.RequiredACR != "" && c.ACR != v.config.RequiredACR {
			return Identity{}, ErrInvalidToken
		}
		if v.config.MaxAuthAge < 0 || (v.config.MaxAuthAge > 0 && (c.AuthTime <= 0 || c.AuthTime > time.Now().Unix() || time.Since(time.Unix(c.AuthTime, 0)) > v.config.MaxAuthAge)) {
			return Identity{}, ErrInvalidToken
		}
		if v.config.ClientID == "" || c.Client != v.config.ClientID || !c.EmailVerified || strings.TrimSpace(c.Email) == "" || c.Kind != "" {
			return Identity{}, ErrInvalidToken
		}
	case ServiceAccount:
		if c.Kind != string(ServiceAccount) || c.Generation == "" {
			return Identity{}, ErrInvalidToken
		}
	default:
		return Identity{}, ErrInvalidToken
	}
	return Identity{Subject: token.Subject, ClientID: c.Client, Email: strings.ToLower(strings.TrimSpace(c.Email)), Name: c.Name, Generation: c.Generation, Scopes: strings.Fields(c.Scope), Kind: v.config.Kind}, nil
}

// Bearer extracts a single, bounded Authorization bearer token. It never trusts
// identity headers forwarded by a browser or gateway.
func Bearer(r *http.Request) (string, error) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return "", ErrInvalidToken
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) > 16384 {
		return "", ErrInvalidToken
	}
	return parts[1], nil
}
