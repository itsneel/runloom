package access

import (
	"context"
	"strings"
	"time"
)

const PlatformAdmin = "platform_admin"
const PlatformStaff = "platform_staff"

type PlatformEvent struct {
	Actor        string `json:"actor"`
	Target       string `json:"target"`
	Action       string `json:"action"`
	PreviousRole string `json:"previous_role"`
	Role         string `json:"role"`
	Reason       string `json:"reason"`
	CreatedAt    string `json:"created_at"`
}

// Platform permissions never participate in OrgRole or TeamRole.
func PlatformPermissions(role string) []string {
	p := []string{}
	if role == PlatformAdmin || role == PlatformStaff {
		p = append(p, "platform.organizations.read", "platform.health.read")
	}
	if role == PlatformAdmin {
		p = append(p, "platform.staff.manage", "platform.audit.read", "platform.organizations.configure")
	}
	return p
}

func HasPlatformPermission(s State, user, permission string) bool {
	for _, p := range PlatformPermissions(s.PlatformRoles[user]) {
		if p == permission {
			return true
		}
	}
	return false
}

func platformAdmins(s *State) int {
	n := 0
	for _, role := range s.PlatformRoles {
		if role == PlatformAdmin {
			n++
		}
	}
	return n
}

// SetPlatformRole rechecks the actor and last-admin invariant within the same
// compare-and-swap transaction as the grant/revocation and its audit event.
func (d *Directory) SetPlatformRole(ctx context.Context, actor User, email, role, reason string) error {
	if role != "" && role != PlatformAdmin && role != PlatformStaff {
		return invalid("invalid platform role")
	}
	reason = strings.TrimSpace(reason)
	if len(reason) < 3 || len(reason) > 300 {
		return invalid("provide a reason between 3 and 300 characters")
	}
	email, err := cleanEmail(email)
	if err != nil {
		return err
	}
	return d.Update(ctx, func(s *State) error {
		if !HasPlatformPermission(*s, actor.ID, "platform.staff.manage") {
			return ErrForbidden
		}
		var target string
		for id, u := range s.Users {
			if u.Email == email {
				if target != "" {
					return invalid("multiple user identities match this email; resolve the identity conflict before assigning platform access")
				}
				target = id
			}
		}
		if target == "" {
			return invalid("user must sign in with a verified email before platform access can be assigned")
		}
		previous := s.PlatformRoles[target]
		if previous == role {
			return nil
		}
		if previous == PlatformAdmin && role != PlatformAdmin && platformAdmins(s) <= 1 {
			return invalid("cannot remove the last platform administrator")
		}
		if s.PlatformRoles == nil {
			s.PlatformRoles = map[string]string{}
		}
		if role == "" {
			delete(s.PlatformRoles, target)
		} else {
			s.PlatformRoles[target] = role
		}
		s.PlatformEvents = append(s.PlatformEvents, PlatformEvent{Actor: actor.ID, Target: target, Action: "platform.role.changed", PreviousRole: previous, Role: role, Reason: reason, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
		return nil
	})
}

// BootstrapPlatformAdmin is an operator-only first-admin operation. There is
// deliberately no HTTP route. Repeated setup cannot restore revoked access.
func (d *Directory) BootstrapPlatformAdmin(ctx context.Context, user User) error {
	return d.Update(ctx, func(s *State) error {
		if s.Users[user.ID].ID == "" {
			return invalid("bootstrap user must already exist")
		}
		if platformAdmins(s) > 0 {
			return nil
		}
		if s.PlatformRoles == nil {
			s.PlatformRoles = map[string]string{}
		}
		s.PlatformRoles[user.ID] = PlatformAdmin
		s.PlatformEvents = append(s.PlatformEvents, PlatformEvent{Actor: "operator:bootstrap", Target: user.ID, Action: "platform.admin.bootstrapped", Role: PlatformAdmin, Reason: "Explicit first platform administrator setup", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
		return nil
	})
}
