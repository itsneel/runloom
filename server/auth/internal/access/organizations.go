package access

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// Bindings hold product authorization, never copies of organization identity or membership.
type OrganizationBinding struct {
	Teams map[string]Team `json:"teams"`
}
type IdentityLease struct {
	Owner     string    `json:"owner"`
	ExpiresAt time.Time `json:"expires_at"`
}

func ChildOrganizationLimit(s State) int {
	if s.MaxChildOrganizations == nil {
		return 2
	}
	return *s.MaxChildOrganizations
}
func (d *Directory) SetChildOrganizationLimit(ctx context.Context, actor User, limit int) error {
	if limit < 0 || limit > 10 {
		return invalid("child organization limit must be between 0 and 10")
	}
	return d.Update(ctx, func(s *State) error {
		if s.PlatformRoles[actor.ID] != PlatformAdmin {
			return ErrForbidden
		}
		previous := ChildOrganizationLimit(*s)
		s.MaxChildOrganizations = &limit
		s.PlatformEvents = append(s.PlatformEvents, PlatformEvent{Actor: actor.ID, Target: "organization_child_limit", Action: "organization.child_limit.changed", Reason: fmt.Sprintf("Child limit changed from %d to %d", previous, limit), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
		return nil
	})
}
func (d *Directory) RenameOrganization(ctx context.Context, actor User, id, name string) error {
	name, err := cleanName(name)
	if err != nil {
		return err
	}
	return d.Update(ctx, func(s *State) error {
		if !IsOrgAdmin(OrgRole(*s, id, actor.ID)) {
			return ErrForbidden
		}
		org := s.Orgs[id]
		if org.Name == name {
			return nil
		}
		old := org.Name
		org.Name = name
		s.Orgs[id] = org
		audit(s, id, actor.ID, "organization.renamed", id)
		s.Events[len(s.Events)-1].OldName, s.Events[len(s.Events)-1].NewName = old, name
		return nil
	})
}

type OrganizationSummary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parent_id,omitempty"`
	Role     string `json:"role,omitempty"`
}
type OrganizationHierarchy struct {
	Root        OrganizationSummary   `json:"root"`
	Children    []OrganizationSummary `json:"children"`
	MaxChildren int                   `json:"max_children"`
}

func Hierarchy(s State, user, id string) (OrganizationHierarchy, error) {
	if OrgRole(s, id, user) == "" {
		return OrganizationHierarchy{}, ErrForbidden
	}
	current := s.Orgs[id]
	root := current
	if current.ParentID != "" {
		var ok bool
		root, ok = s.Orgs[current.ParentID]
		if !ok {
			return OrganizationHierarchy{}, invalid("parent organization is unavailable")
		}
	}
	summary := func(o Organization) OrganizationSummary {
		return OrganizationSummary{ID: o.ID, Name: o.Name, ParentID: o.ParentID, Role: OrgRole(s, o.ID, user)}
	}
	out := OrganizationHierarchy{Root: summary(root), Children: []OrganizationSummary{}, MaxChildren: OrganizationChildLimit(s, root.ID)}
	for _, o := range s.Orgs {
		if o.ParentID == root.ID {
			out.Children = append(out.Children, summary(o))
		}
	}
	sort.Slice(out.Children, func(i, j int) bool { return out.Children[i].Name < out.Children[j].Name })
	return out, nil
}
