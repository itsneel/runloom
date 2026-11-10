package access

import (
	"context"
	"fmt"
	"time"
)

// These are Runloom creation policies. Keycloak remains the organization directory.
type OrganizationChildPolicy struct {
	MaxChildren        int  `json:"max_children"`
	DefaultMaxChildren int  `json:"default_max_children"`
	Override           *int `json:"override"`
	Maximum            int  `json:"maximum"`
}

func OrganizationChildLimit(s State, id string) int {
	if limit, ok := s.ChildOrganizationOverrides[id]; ok {
		return limit
	}
	return ChildOrganizationLimit(s)
}

func ChildOrganizationPolicy(s State, id string) (OrganizationChildPolicy, error) {
	org, ok := s.Orgs[id]
	if !ok {
		return OrganizationChildPolicy{}, ErrNotFound
	}
	if org.ParentID != "" {
		return OrganizationChildPolicy{}, invalid("child organizations cannot contain child organizations")
	}
	policy := OrganizationChildPolicy{MaxChildren: OrganizationChildLimit(s, id), DefaultMaxChildren: ChildOrganizationLimit(s), Maximum: 10}
	if limit, ok := s.ChildOrganizationOverrides[id]; ok {
		policy.Override = &limit
	}
	return policy, nil
}

// A nil limit restores inheritance; zero explicitly blocks new children.
func (d *Directory) SetOrganizationChildLimit(ctx context.Context, actor User, id string, limit *int) error {
	return d.Update(ctx, func(s *State) error {
		if s.PlatformRoles[actor.ID] != PlatformAdmin {
			return ErrForbidden
		}
		if limit != nil && (*limit < 0 || *limit > 10) {
			return invalid("child organization limit must be between 0 and 10")
		}
		previous, err := ChildOrganizationPolicy(*s, id)
		if err != nil {
			return err
		}
		if limit == nil && previous.Override == nil || limit != nil && previous.Override != nil && *limit == *previous.Override {
			return nil
		}
		if s.ChildOrganizationOverrides == nil {
			s.ChildOrganizationOverrides = map[string]int{}
		}
		action, source := "organization.child_limit.override.changed", "organization override"
		if limit == nil {
			delete(s.ChildOrganizationOverrides, id)
			action, source = "organization.child_limit.override.reset", "platform default"
		} else {
			s.ChildOrganizationOverrides[id] = *limit
		}
		oldSource := "platform default"
		if previous.Override != nil {
			oldSource = "organization override"
		}
		s.PlatformEvents = append(s.PlatformEvents, PlatformEvent{Actor: actor.ID, Target: id, Action: action, Reason: fmt.Sprintf("Child limit changed from %d (%s) to %d (%s)", previous.MaxChildren, oldSource, OrganizationChildLimit(*s, id), source), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
		return nil
	})
}
