package chat

import (
	"context"
	"encoding/json"
	"errors"
	runloomauth "github.com/example/runloom/sdk/go-auth"
	observability "github.com/example/runloom/sdk/go-observability"
	"io"
	"net/http"
	"strings"
	"time"
)

var ErrUnauthorized = errors.New("please sign in again")
var ErrForbidden = errors.New("you do not have access to this team")

type Authorize func(context.Context, *http.Request, string, string) (Scope, error)

// Authentication is verified locally; live membership comes from the owner API.
// No caller identity headers, privileged service token or persisted user token.
func Authorization(v *runloomauth.Verifier, authURL string) Authorize {
	client := &http.Client{Transport: observability.InternalTransport(nil, authURL), Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return func(ctx context.Context, r *http.Request, org, team string) (Scope, error) {
		raw, err := runloomauth.Bearer(r)
		if err != nil {
			return Scope{}, ErrUnauthorized
		}
		identity, err := v.Verify(ctx, raw)
		if err != nil {
			return Scope{}, ErrUnauthorized
		}
		req, _ := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(authURL, "/")+"/api/v1/studio/me", nil)
		req.Header.Set("Authorization", "Bearer "+raw)
		res, err := client.Do(req)
		if err != nil {
			return Scope{}, errors.New("access service unavailable")
		}
		defer res.Body.Close()
		if res.StatusCode == 401 {
			return Scope{}, ErrUnauthorized
		}
		if res.StatusCode != 200 {
			return Scope{}, errors.New("access service unavailable")
		}
		var overview struct {
			User struct {
				ID string `json:"id"`
			} `json:"user"`
			Organizations []struct {
				ID    string `json:"id"`
				Teams []struct {
					ID   string `json:"id"`
					Role string `json:"role"`
				} `json:"teams"`
			} `json:"organizations"`
		}
		if json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&overview) != nil || overview.User.ID != identity.Subject {
			return Scope{}, ErrUnauthorized
		}
		for _, o := range overview.Organizations {
			if o.ID == org {
				for _, t := range o.Teams {
					if t.ID == team && t.Role != "" {
						return Scope{User: identity.Subject, Org: org, Team: team}, nil
					}
				}
			}
		}
		return Scope{}, ErrForbidden
	}
}
