package chat

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	observability "github.com/example/runloom/sdk/go-observability"
)

var workflowID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
var ErrWorkflowUnavailable = errors.New("Workflow access check unavailable. Please retry.")
var ErrWorkflowNotFound = errors.New("Workflow is unavailable or you no longer have access. Save a new draft before using workflow chat.")

type WorkflowAccess func(context.Context, *http.Request, Scope) error

// The workflow owner checks the caller's live permissions. Only forward their
// bearer and the authorized team, never privileged credentials or caller URLs.
// Response bodies (drafts, nodes and inline inputs) do not enter chat/model data.
func WorkflowAuthorization(base string) WorkflowAccess {
	u, err := url.Parse(base)
	valid := err == nil && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Scheme == "http" || u.Scheme == "https")
	client := &http.Client{Transport: observability.InternalTransport(nil, base), Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return func(ctx context.Context, r *http.Request, scope Scope) error {
		if !valid {
			return ErrWorkflowUnavailable
		}
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		// A definition can have a saved draft, a published version, or both.
		// Retry only a missing draft, never a denial or dependency failure.
		for _, suffix := range []string{"?version=0", ""} {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/api/v1/studio/definitions/"+url.PathEscape(scope.Workflow)+suffix, nil)
			if err != nil {
				return ErrWorkflowUnavailable
			}
			req.Header.Set("Authorization", r.Header.Get("Authorization"))
			req.Header.Set("X-Runloom-Team", scope.Team)
			res, err := client.Do(req)
			if err != nil {
				return ErrWorkflowUnavailable
			}
			res.Body.Close()
			switch res.StatusCode {
			case http.StatusOK:
				return nil
			case http.StatusUnauthorized:
				return ErrUnauthorized
			case http.StatusForbidden:
				return ErrWorkflowNotFound
			case http.StatusNotFound:
				continue
			default:
				return ErrWorkflowUnavailable
			}
		}
		return ErrWorkflowNotFound
	}
}
