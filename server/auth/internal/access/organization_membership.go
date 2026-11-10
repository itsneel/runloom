package access

import "strings"

func organizationFamily(o Organization) string {
	if o.ParentID != "" {
		return o.ParentID
	}
	return o.ID
}

func samePerson(a, b Member) bool {
	return a.UserID != "" && a.UserID == b.UserID ||
		a.Email != "" && strings.EqualFold(a.Email, b.Email)
}

// Validate new grants (including pending invitations and invitation acceptance).
// Existing grants can still be removed to repair old or operator-created data.
func validateOrganizationMembershipChanges(previous, next State) error {
	for id, org := range next.Orgs {
		old := previous.Orgs[id]
		for email, member := range org.Members {
			prior, exists := old.Members[email]
			if exists && prior.UserID == member.UserID && prior.Email == member.Email && old.ParentID == org.ParentID {
				continue
			}
			for otherID, other := range next.Orgs {
				if otherID == id || organizationFamily(other) == organizationFamily(org) {
					continue
				}
				for _, candidate := range other.Members {
					if samePerson(member, candidate) {
						return invalid("a user can belong to only one organization family; remove memberships and pending invitations in the other family first")
					}
				}
			}
		}
	}
	return nil
}

// Only users without memberships or pending invitations may create a root.
func CanCreateRootOrganization(s State, user User) bool {
	if user.ID == "" {
		return false
	}
	for _, org := range s.Orgs {
		for _, member := range org.Members {
			if samePerson(member, Member{UserID: user.ID, Email: user.Email}) {
				return false
			}
		}
	}
	return true
}
