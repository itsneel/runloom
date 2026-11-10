package services

import (
	"context"
	"github.com/example/runloom/management/internal/access"
)

// directoryView is a read-only API projection. Only workflow secrets have a
// local repository here; identity and service-account writes are never proxied.
type directoryView struct {
	client  *Client
	secrets access.Repository
	scope   string
}

func remoteDirectory(client *Client, secrets access.Repository) *access.Directory {
	r := &directoryView{client: client, secrets: secrets, scope: "identity"}
	return &access.Directory{Repo: r, BindRemote: func(ctx context.Context, _ access.User) (access.State, error) {
		var s access.State
		err := client.call(ctx, "identity/bind", map[string]string{"token": access.Bearer(ctx)}, &s)
		return s, err
	}}
}
func (r *directoryView) Module(scope string) access.Repository {
	if scope == "secrets" && r.secrets != nil {
		return r.secrets.Module(scope)
	}
	return &directoryView{client: r.client, secrets: r.secrets, scope: scope}
}
func (r *directoryView) ReadOwned(ctx context.Context) (access.State, int64, error) {
	var s access.State
	if r.scope != "identity" && r.scope != "consumers" {
		return s, 0, access.ErrForbidden
	}
	err := r.client.call(ctx, "identity/view", map[string]string{"scope": r.scope}, &s)
	return s, 0, err
}
func (r *directoryView) Read(ctx context.Context) (access.State, int64, error) {
	s, rev, err := r.ReadOwned(ctx)
	if err != nil {
		return s, rev, err
	}
	if r.scope == "identity" && r.secrets != nil {
		part, _, e := r.secrets.Module("secrets").ReadOwned(ctx)
		if e != nil {
			return s, rev, e
		}
		s.Secrets = part.Secrets
	}
	return s, rev, nil
}
func (r *directoryView) Swap(context.Context, int64, access.State) (bool, error) {
	return false, access.ErrForbidden
}
func (r *directoryView) Close(context.Context) error { return nil }
