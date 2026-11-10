package consumer

import (
	"context"
	runloomauth "github.com/example/runloom/sdk/go-auth"
)

type Verifier interface {
	Verify(context.Context, string) (Identity, error)
}
type tokenVerifier struct{ verifier *runloomauth.Verifier }

func NewVerifier(ctx context.Context, issuer, jwks, audience string) Verifier {
	return &tokenVerifier{runloomauth.New(ctx, runloomauth.Config{Issuer: issuer, JWKSURL: jwks, Audience: audience, Kind: runloomauth.ServiceAccount})}
}
func (v *tokenVerifier) Verify(ctx context.Context, raw string) (Identity, error) {
	u, err := v.verifier.Verify(ctx, raw)
	if err != nil {
		return Identity{}, err
	}
	return Identity{ClientID: u.ClientID, Subject: u.Subject, Generation: u.Generation, Scopes: u.Scopes}, nil
}
