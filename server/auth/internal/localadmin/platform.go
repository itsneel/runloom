package localadmin

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/example/runloom/auth/internal/access"
)

// Public development defaults, available only through the guarded local setup CLI.
const PlatformAdminEmail = "platform-admin@runloom.test"
const PlatformAdminPassword = "RunloomPlatformAdmin!123"
const PlatformStaffEmail = "platform-staff@runloom.test"
const PlatformStaffPassword = "RunloomPlatformStaff!123"

type PlatformAccountResult struct {
	Email           string `json:"email"`
	DefaultPassword string `json:"default_password"`
	Created         bool   `json:"created"`
	Reset           bool   `json:"reset"`
	Role            string `json:"role"`
}

func EnsurePlatformAccounts(ctx context.Context, base string, d *access.Directory, reset bool) (map[string]PlatformAccountResult, error) {
	results := map[string]PlatformAccountResult{}
	for _, account := range []struct{ email, password, name, role string }{
		{PlatformAdminEmail, PlatformAdminPassword, "Local Platform Admin", access.PlatformAdmin},
		{PlatformStaffEmail, PlatformStaffPassword, "Local Platform Staff", access.PlatformStaff},
	} {
		user, created, err := ensureIdentity(ctx, base, reset, account.email, account.password, account.name)
		if err != nil {
			return nil, err
		}
		if err := EnsurePlatformDirectory(ctx, d, user); err != nil {
			return nil, err
		}
		state, _, err := d.Repo.Read(ctx)
		if err != nil {
			return nil, errors.New("cannot read local platform account permissions")
		}
		results[account.role] = PlatformAccountResult{Email: account.email, DefaultPassword: account.password, Created: created, Reset: reset, Role: state.PlatformRoles[user.ID]}
	}
	return results, nil
}

// EnsurePlatformDirectory is local operator provisioning, not an API operation.
// It grants once, atomically with an audit marker, without tenant membership.
// Existing role assignments, demotions and revocations are never overwritten.
func EnsurePlatformDirectory(ctx context.Context, d *access.Directory, user access.User) error {
	role := ""
	switch user.Email {
	case PlatformAdminEmail:
		role = access.PlatformAdmin
	case PlatformStaffEmail:
		role = access.PlatformStaff
	default:
		return errors.New("invalid local platform account email")
	}
	if user.ID == "" {
		return errors.New("invalid local platform account subject")
	}
	return d.Update(ctx, func(s *access.State) error {
		for id, existing := range s.Users {
			if (id == user.ID && existing.Email != user.Email) || (id != user.ID && strings.EqualFold(existing.Email, user.Email)) {
				return errors.New("local platform account belongs to a different identity; refusing to rebind")
			}
		}
		previous := s.PlatformRoles[user.ID]
		hasHistory := false
		for _, event := range s.PlatformEvents {
			if event.Target != user.ID {
				continue
			}
			if event.Action == "local.platform.account.ensured" {
				return nil
			}
			hasHistory = true
		}
		if s.Users == nil {
			s.Users = map[string]access.User{}
		}
		if s.PlatformRoles == nil {
			s.PlatformRoles = map[string]string{}
		}
		if _, exists := s.Users[user.ID]; !exists {
			s.Users[user.ID] = user
		}
		if previous == "" && !hasHistory {
			s.PlatformRoles[user.ID] = role
		}
		s.PlatformEvents = append(s.PlatformEvents, access.PlatformEvent{
			Actor: "operator:local-bootstrap", Target: user.ID, Action: "local.platform.account.ensured",
			PreviousRole: previous, Role: s.PlatformRoles[user.ID], Reason: "Local development account setup; existing access preserved",
			CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		})
		return nil
	})
}

// SeparateOrganizationAdmin removes the legacy local bootstrap grant only after
// the dedicated platform administrator exists. Tenant roles and identities stay
// unchanged. This is called only by the guarded local setup executable.
func SeparateOrganizationAdmin(ctx context.Context, d *access.Directory, user access.User) error {
	if user.ID == "" || user.Email != Email {
		return errors.New("invalid local organization administrator")
	}
	return d.Update(ctx, func(s *access.State) error {
		if s.Users[user.ID].Email != Email {
			return errors.New("local organization administrator identity mismatch")
		}
		for id, existing := range s.Users {
			if id != user.ID && strings.EqualFold(existing.Email, Email) {
				return errors.New("ambiguous local organization administrator identity")
			}
		}
		previous := s.PlatformRoles[user.ID]
		if previous == "" {
			return nil
		}
		dedicatedAdmin := false
		for id, existing := range s.Users {
			if existing.Email == PlatformAdminEmail && s.PlatformRoles[id] == access.PlatformAdmin {
				dedicatedAdmin = true
			}
		}
		if !dedicatedAdmin {
			return errors.New("dedicated local platform administrator must have active platform_admin access before removing the legacy organization-admin grant")
		}
		delete(s.PlatformRoles, user.ID)
		s.PlatformEvents = append(s.PlatformEvents, access.PlatformEvent{
			Actor: "operator:local-bootstrap", Target: user.ID, Action: "platform.role.changed",
			PreviousRole: previous, Role: "", Reason: "Separate local organization administrator from dedicated platform accounts",
			CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		})
		return nil
	})
}
