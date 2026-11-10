package access

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Only product access bindings, audit, settings and a short operation lease are
// durable here. Organization names, hierarchy, roles and native membership are
// never written to the application database after migration.
type keycloakRepository struct {
	base      Repository
	provider  *KeycloakDirectory
	migration sync.Mutex
	migrated  bool
}

func WithKeycloakOrganizations(base Repository, provider *KeycloakDirectory) Repository {
	return &keycloakRepository{base: base, provider: provider}
}
func (r *keycloakRepository) Module(scope string) Repository {
	if scope == "identity" {
		return r
	}
	return r.base.Module(scope)
}
func (r *keycloakRepository) Close(ctx context.Context) error { return r.base.Close(ctx) }
func projectionRevision(s State, revision int64) int64 {
	s.IdentityLease = nil
	raw, _ := json.Marshal(struct {
		Revision int64
		State    State
	}{revision, s})
	hash := sha256.Sum256(raw)
	return int64(binary.LittleEndian.Uint64(hash[:8]) & 0x7fffffffffffffff)
}
func (r *keycloakRepository) acquire(ctx context.Context) (State, int64, string, error) {
	token := uuid.NewString()
	for {
		s, rev, err := r.base.ReadOwned(ctx)
		if err != nil {
			return s, 0, "", err
		}
		if s.IdentityLease == nil || time.Now().After(s.IdentityLease.ExpiresAt) {
			s.IdentityLease = &IdentityLease{Owner: token, ExpiresAt: time.Now().Add(2 * time.Minute)}
			ok, err := r.base.Swap(ctx, rev, s)
			if err != nil {
				return s, 0, "", err
			}
			if ok {
				s.IdentityLease = nil
				return s, rev, token, nil
			}
		}
		select {
		case <-ctx.Done():
			return State{}, 0, "", ctx.Err()
		case <-time.After(30 * time.Millisecond):
		}
	}
}
func (r *keycloakRepository) release(token string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := 0; i < 16; i++ {
		s, rev, err := r.base.ReadOwned(ctx)
		if err != nil || s.IdentityLease == nil || s.IdentityLease.Owner != token {
			return
		}
		s.IdentityLease = nil
		ok, err := r.base.Swap(ctx, rev, s)
		if err != nil || ok {
			return
		}
	}
}
func bindings(s State) map[string]OrganizationBinding {
	out := map[string]OrganizationBinding{}
	for id, o := range s.Orgs {
		out[id] = OrganizationBinding{Teams: o.Teams}
	}
	return out
}
func (r *keycloakRepository) migrate(ctx context.Context) error {
	r.migration.Lock()
	defer r.migration.Unlock()
	if r.migrated {
		return nil
	}
	s, _, err := r.base.ReadOwned(ctx)
	if err != nil {
		return err
	}
	if len(s.Orgs) == 0 {
		r.migrated = true
		return nil
	}
	s, rev, token, err := r.acquire(ctx)
	if err != nil {
		return err
	}
	defer r.release(token)
	native, err := r.provider.organizations(ctx)
	if err != nil {
		return err
	}
	for id, o := range s.Orgs {
		var prior *nativeOrganization
		if p, ok := native[id]; ok {
			prior = &p
		}
		if err = r.provider.save(ctx, o, prior); err != nil {
			return fmt.Errorf("organization migration to Keycloak did not complete: %w", err)
		}
	}
	if s.OrganizationBindings == nil {
		s.OrganizationBindings = map[string]OrganizationBinding{}
	}
	for id, b := range bindings(s) {
		s.OrganizationBindings[id] = b
	}
	s.Orgs = nil
	s.IdentityLease = nil
	ok, err := r.base.Swap(ctx, rev+1, s)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("organization migration metadata changed concurrently")
	}
	r.migrated = true
	return nil
}
func (r *keycloakRepository) ReadOwned(ctx context.Context) (State, int64, error) {
	if err := r.migrate(ctx); err != nil {
		return State{}, 0, err
	}
	s, rev, err := r.base.ReadOwned(ctx)
	if err != nil {
		return s, 0, err
	}
	s.IdentityLease = nil
	s, _, err = r.provider.load(ctx, s)
	if err != nil {
		return s, 0, err
	}
	return s, projectionRevision(s, rev), nil
}
func (r *keycloakRepository) Read(ctx context.Context) (State, int64, error) {
	s, rev, err := r.ReadOwned(ctx)
	if err != nil {
		return s, rev, err
	}
	for _, scope := range []string{"secrets", "consumers"} {
		part, _, e := r.base.Module(scope).Read(ctx)
		if e != nil {
			return s, rev, e
		}
		if scope == "secrets" {
			s.Secrets = part.Secrets
		} else {
			s.ServiceAccounts = part.ServiceAccounts
		}
		s.Events = append(s.Events, part.Events...)
	}
	return s, rev, nil
}
func (r *keycloakRepository) Swap(ctx context.Context, revision int64, next State) (bool, error) {
	base, baseRev, token, err := r.acquire(ctx)
	if err != nil {
		return false, err
	}
	defer r.release(token)
	current, native, err := r.provider.load(ctx, base)
	if err != nil {
		return false, err
	}
	if projectionRevision(current, baseRev) != revision {
		return false, nil
	}
	for id, o := range next.Orgs {
		previous, exists := current.Orgs[id]
		if exists && sameOrganizationIdentity(previous, o) {
			continue
		}
		var prior *nativeOrganization
		if p, ok := native[id]; ok {
			prior = &p
		}
		if err = r.provider.save(ctx, o, prior); err != nil {
			return false, err
		}
	}
	for id := range current.Orgs {
		if _, ok := next.Orgs[id]; !ok {
			return false, invalid("organization deletion requires an explicit operator lifecycle process")
		}
	}
	// No provider records are copied into durable application metadata.
	next.OrganizationBindings = base.OrganizationBindings
	if next.OrganizationBindings == nil {
		next.OrganizationBindings = map[string]OrganizationBinding{}
	}
	for id, b := range bindings(next) {
		next.OrganizationBindings[id] = b
	}
	next.Orgs = nil
	next.IdentityLease = nil
	existing, storedRev, err := r.base.ReadOwned(ctx)
	if err != nil {
		return false, err
	}
	if existing.IdentityLease == nil || existing.IdentityLease.Owner != token {
		return false, fmt.Errorf("organization operation lease expired")
	}
	existing.IdentityLease = nil
	if reflect.DeepEqual(existing, next) {
		return true, nil
	}
	return r.base.Swap(ctx, storedRev, next)
}
