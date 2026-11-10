package access

import (
	"context"
	"errors"
	"github.com/example/runloom/auth/internal/config"
	runloomauth "github.com/example/runloom/sdk/go-auth"
	"time"
)

type unavailablePlatformVerifier struct{}

func (unavailablePlatformVerifier) Verify(context.Context, string) (User, error) {
	return User{}, errors.New("platform MFA not configured")
}

// Password-only access is an explicit local-setup opt-in. Hosted deployments
// must map the required ACR to an MFA authentication flow in their identity provider.
func NewPlatformOIDC(ctx context.Context, c Config) Verifier {
	if config.Value("RUNLOOM_PLATFORM_ALLOW_PASSWORD_ONLY") == "true" {
		return NewOIDC(ctx, c)
	}
	acr := config.Value("RUNLOOM_PLATFORM_REQUIRED_ACR")
	if acr == "" {
		return unavailablePlatformVerifier{}
	}
	age := 15 * time.Minute
	if text := config.Value("RUNLOOM_PLATFORM_MAX_AUTH_AGE"); text != "" {
		var err error
		age, err = time.ParseDuration(text)
		if err != nil || age <= 0 {
			return unavailablePlatformVerifier{}
		}
	}
	return &OIDC{ClientID: c.ClientID, verifier: runloomauth.New(ctx, runloomauth.Config{Issuer: c.Issuer, JWKSURL: c.JWKSURL, Audience: c.ClientID, ClientID: c.ClientID, Kind: runloomauth.Human, RequiredACR: acr, MaxAuthAge: age})}
}
