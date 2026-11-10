package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const EditLeaseTTL = 90 * time.Second

var ErrEditLocked = errors.New("workflow is locked by another editing session")
var ErrEditLease = errors.New("editing session expired or is no longer owned; reload before editing")

// Stored only in team metadata; never serialize this record into public responses.
type EditLease struct {
	Owner     string    `json:"owner" bson:"owner"`
	Name      string    `json:"name" bson:"name"`
	Token     string    `json:"token" bson:"token"`
	ExpiresAt time.Time `json:"expires_at" bson:"expires_at"`
	Operation string    `json:"operation,omitempty" bson:"operation,omitempty"`
	Ephemeral bool      `json:"ephemeral,omitempty" bson:"ephemeral,omitempty"`
}

type EditLockStatus struct {
	Locked     bool       `json:"locked"`
	Owner      string     `json:"owner,omitempty"`
	OwnerName  string     `json:"owner_name,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	Writing    bool       `json:"writing"`
	TTLSeconds int        `json:"ttl_seconds"`
}

func leaseStatus(l EditLease) EditLockStatus {
	if l.Operation == "" && !l.ExpiresAt.After(time.Now()) {
		return EditLockStatus{}
	}
	return EditLockStatus{Locked: true, Owner: l.Owner, OwnerName: l.Name, ExpiresAt: &l.ExpiresAt, Writing: l.Operation != "", TTLSeconds: int(EditLeaseTTL.Seconds())}
}

func (s *projectStore) EditLock(ctx context.Context, id string) (EditLockStatus, error) {
	if !projectDefinitionID.MatchString(id) {
		return EditLockStatus{}, ErrNotFound
	}
	c, _, err := s.read(ctx, "leases")
	return leaseStatus(c.EditLocks[id]), err
}

func (s *projectStore) ChangeEditLock(ctx context.Context, id, owner, name, token, action string) (EditLockStatus, error) {
	if !projectDefinitionID.MatchString(id) {
		return EditLockStatus{}, ErrNotFound
	}
	if _, err := uuid.Parse(token); err != nil || owner == "" {
		return EditLockStatus{}, ErrEditLease
	}
	var result EditLockStatus
	err := s.updateScope(ctx, "leases", func(c *ProjectCatalog) error {
		l := c.EditLocks[id]
		result = leaseStatus(l)
		owned := l.Owner == owner && l.Token == token
		switch action {
		case "acquire":
			if result.Locked && !owned {
				return ErrEditLocked
			}
			// Acquisition is idempotent only while the original session is live.
			// An expired token must not resurrect an old browser's unsaved content.
			if owned && !l.ExpiresAt.After(time.Now()) {
				return ErrEditLease
			}
		case "renew", "release":
			if !owned || !result.Locked || !l.ExpiresAt.After(time.Now()) {
				return ErrEditLease
			}
			if action == "release" {
				if l.Operation != "" {
					return ErrEditLocked
				}
				delete(c.EditLocks, id)
				result = EditLockStatus{}
				return nil
			}
		default:
			return fmt.Errorf("invalid edit lock action")
		}
		if c.EditLocks == nil {
			c.EditLocks = map[string]EditLease{}
		}
		if len(c.EditLocks) >= 10000 && l.Token == "" {
			for key, value := range c.EditLocks {
				if !leaseStatus(value).Locked {
					delete(c.EditLocks, key)
				}
			}
			if len(c.EditLocks) >= 10000 {
				return fmt.Errorf("too many editing sessions")
			}
		}
		l.Owner, l.Name, l.Token = owner, name, token
		l.ExpiresAt = time.Now().UTC().Add(EditLeaseTTL)
		c.EditLocks[id] = l
		result = leaseStatus(l)
		return nil
	})
	return result, err
}

// Reserve the whole mutation with CAS, not a check-then-write. A reservation
// never expires mid-write: delayed processes must not race a replacement editor.
// After an API crash during a write it deliberately fails closed; operators must
// stop writers and inspect the definition before clearing its operation record.
// Legacy API writes without a token get a single-operation lock only when no
// editor holds the workflow. The authenticated API is the mutation boundary.
func (s *projectStore) BeginDefinitionWrite(ctx context.Context, id, owner, token string) (func() error, error) {
	if !projectDefinitionID.MatchString(id) {
		return nil, ErrNotFound
	}
	op := uuid.NewString()
	err := s.updateScope(ctx, "leases", func(c *ProjectCatalog) error {
		l := c.EditLocks[id]
		if l.Operation != "" {
			return ErrEditLocked
		}
		if token != "" {
			if l.Token != token || l.Owner != owner || !l.ExpiresAt.After(time.Now()) {
				return ErrEditLease
			}
		} else {
			if leaseStatus(l).Locked {
				return ErrEditLocked
			}
			l = EditLease{Owner: owner, Name: owner, Token: op, Ephemeral: true, ExpiresAt: time.Now().UTC().Add(EditLeaseTTL)}
		}
		if c.EditLocks == nil {
			c.EditLocks = map[string]EditLease{}
		}
		l.Operation = op
		c.EditLocks[id] = l
		return nil
	})
	if err != nil {
		return nil, err
	}
	return func() error {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.updateScope(cleanup, "leases", func(c *ProjectCatalog) error {
			l := c.EditLocks[id]
			if l.Operation != op {
				return ErrEditLease
			}
			if l.Ephemeral {
				delete(c.EditLocks, id)
			} else {
				l.Operation = ""
				c.EditLocks[id] = l
			}
			return nil
		})
	}, nil
}
