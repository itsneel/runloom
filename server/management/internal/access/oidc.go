package access

import (
	"context"
	"github.com/example/runloom/management/internal/config"
	runloomauth "github.com/example/runloom/sdk/go-auth"
	"time"
)

type Verifier interface {
	Verify(context.Context, string) (User, error)
}
type OIDC struct {
	verifier *runloomauth.Verifier
	ClientID string
}
type Config struct {
	Issuer        string
	JWKSURL       string
	ClientID      string
	URL           string
	Realm         string
	GoogleEnabled bool
}

func ConfigFromEnv() Config {
	c := Config{Issuer: config.Value("RUNLOOM_OIDC_ISSUER"), JWKSURL: config.Value("RUNLOOM_OIDC_JWKS_URL"), ClientID: config.Value("RUNLOOM_OIDC_CLIENT_ID"), URL: config.Value("RUNLOOM_OIDC_URL"), Realm: config.Value("RUNLOOM_OIDC_REALM"), GoogleEnabled: config.Value("RUNLOOM_GOOGLE_LOGIN_ENABLED") == "true"}
	if c.Issuer == "" {
		c.Issuer = "http://localhost:4200/auth/realms/runloom"
	}
	if c.JWKSURL == "" {
		c.JWKSURL = c.Issuer + "/protocol/openid-connect/certs"
	}
	if c.ClientID == "" {
		c.ClientID = "runloom"
	}
	if c.Realm == "" {
		c.Realm = "runloom"
	}
	if c.URL == "" {
		c.URL = "/auth"
	}
	return c
}
func NewOIDC(ctx context.Context, c Config) *OIDC {
	var age time.Duration
	if value := config.Value("RUNLOOM_OIDC_MAX_AUTH_AGE"); value != "" {
		var err error
		age, err = time.ParseDuration(value)
		if err != nil || age <= 0 {
			age = -1
		}
	}
	return &OIDC{verifier: runloomauth.New(ctx, runloomauth.Config{Issuer: c.Issuer, JWKSURL: c.JWKSURL, Audience: c.ClientID, ClientID: c.ClientID, Kind: runloomauth.Human, RequiredACR: config.Value("RUNLOOM_OIDC_REQUIRED_ACR"), MaxAuthAge: age}), ClientID: c.ClientID}
}
func (v *OIDC) Verify(ctx context.Context, raw string) (User, error) {
	u, err := v.verifier.Verify(ctx, raw)
	if err != nil {
		return User{}, err
	}
	return User{ID: u.Subject, Email: u.Email, Name: u.Name}, nil
}
