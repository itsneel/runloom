package access

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"
)

func (k *KeycloakDirectory) load(ctx context.Context, base State) (State, map[string]nativeOrganization, error) {
	native, err := k.organizations(ctx)
	if err != nil {
		return base, nil, err
	}
	base.Orgs = map[string]Organization{}
	for id, o := range native {
		if !o.Enabled {
			continue
		}
		members, err := k.members(ctx, o.ID)
		if err != nil {
			return base, nil, err
		}
		org := Organization{ID: id, Name: o.Name, ParentID: attribute(o, "runloom.parent_id"), CreatedBy: attribute(o, "runloom.created_by"), Members: map[string]Member{}, Teams: map[string]Team{}}
		roles := map[string]string{}
		for _, u := range o.Attributes["runloom.admins"] {
			roles[u] = "admin"
		}
		for _, u := range o.Attributes["runloom.owners"] {
			roles[u] = "owner"
		}
		for _, m := range members {
			role := roles[m.ID]
			if role == "" {
				role = "member"
			}
			org.Members[strings.ToLower(m.Email)] = Member{Email: strings.ToLower(m.Email), UserID: m.ID, Role: role}
		}
		for _, raw := range o.Attributes["runloom.pending_members"] {
			var m Member
			if json.Unmarshal([]byte(raw), &m) != nil || m.UserID != "" {
				return base, nil, fmt.Errorf("invalid pending organization membership in Keycloak")
			}
			if _, ok := org.Members[m.Email]; !ok {
				org.Members[m.Email] = m
			}
		}
		if binding, ok := base.OrganizationBindings[id]; ok {
			org.Teams = binding.Teams
			for teamID, team := range org.Teams {
				for email, m := range team.Members {
					om, exists := org.Members[email]
					if !exists || m.UserID != "" && om.UserID != m.UserID {
						delete(team.Members, email)
					}
				}
				org.Teams[teamID] = team
			}
		}
		base.Orgs[id] = org
	}
	return base, native, nil
}
func sameOrganizationIdentity(a, b Organization) bool {
	a.Teams = nil
	b.Teams = nil
	return reflect.DeepEqual(a, b)
}

// Metadata is editable in Keycloak. Membership is native; role/hierarchy
// attributes do not grant access unless the user is also a native member.
func (k *KeycloakDirectory) save(ctx context.Context, org Organization, previous *nativeOrganization) error {
	current := nativeOrganization{Name: org.Name, Alias: "runloom-" + org.ID, Enabled: true, Attributes: map[string][]string{}}
	if previous != nil {
		current = *previous
		current.Name = org.Name
		current.Attributes = map[string][]string{}
		for key, value := range previous.Attributes {
			current.Attributes[key] = value
		}
	}
	attrs := current.Attributes
	attrs["runloom.id"] = []string{org.ID}
	attrs["runloom.parent_id"] = []string{org.ParentID}
	attrs["runloom.created_by"] = []string{org.CreatedBy}
	attrs["runloom.owners"], attrs["runloom.admins"], attrs["runloom.pending_members"] = []string{}, []string{}, []string{}
	desired := map[string]bool{}
	for _, m := range org.Members {
		if m.UserID == "" {
			raw, _ := json.Marshal(m)
			attrs["runloom.pending_members"] = append(attrs["runloom.pending_members"], string(raw))
			continue
		}
		desired[m.UserID] = true
		if m.Role == "owner" {
			attrs["runloom.owners"] = append(attrs["runloom.owners"], m.UserID)
		} else if m.Role == "admin" {
			attrs["runloom.admins"] = append(attrs["runloom.admins"], m.UserID)
		}
	}
	for _, key := range []string{"runloom.owners", "runloom.admins", "runloom.pending_members"} {
		sort.Strings(attrs[key])
	}
	created := false
	if previous == nil {
		header, err := k.call(ctx, "POST", "/organizations", current, nil)
		if err != nil {
			return err
		}
		location, err := url.Parse(header.Get("Location"))
		if err != nil {
			return fmt.Errorf("Keycloak omitted organization identity")
		}
		parts := strings.Split(strings.TrimRight(location.Path, "/"), "/")
		current.ID = parts[len(parts)-1]
		if current.ID == "" {
			return fmt.Errorf("Keycloak omitted organization identity")
		}
		created = true
	} else {
		if _, err := k.call(ctx, "PUT", "/organizations/"+url.PathEscape(current.ID), current, nil); err != nil {
			return err
		}
	}
	members, err := k.members(ctx, current.ID)
	if err != nil {
		return err
	}
	existing := map[string]bool{}
	for _, m := range members {
		existing[m.ID] = true
	}
	for id := range desired {
		if !existing[id] {
			if _, err = k.call(ctx, "POST", "/organizations/"+url.PathEscape(current.ID)+"/members", id, nil); err != nil {
				if created {
					_, _ = k.call(ctx, "DELETE", "/organizations/"+url.PathEscape(current.ID), nil, nil)
				}
				return err
			}
		}
	}
	for id := range existing {
		if !desired[id] {
			if _, err = k.call(ctx, "DELETE", "/organizations/"+url.PathEscape(current.ID)+"/members/"+url.PathEscape(id), nil, nil); err != nil {
				return err
			}
		}
	}
	return nil
}
