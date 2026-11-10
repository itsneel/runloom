package api

import (
	"encoding/json"
	"testing"

	"github.com/example/runloom/auth/internal/access"
	"github.com/stretchr/testify/require"
)

func TestPerOrganizationChildLimits(t *testing.T) {
	g := membershipGateway(t)
	owner := access.User{ID: "owner", Email: "owner@example.test"}
	other, err := g.Directory.CreateOrganization(t.Context(), access.User{ID: "new-user", Email: "new-user@example.test"}, "Other parent", "")
	require.NoError(t, err)
	child, err := g.Directory.CreateOrganization(t.Context(), owner, "Existing child", "org")
	require.NoError(t, err)
	require.NoError(t, g.Directory.Update(t.Context(), func(s *access.State) error {
		s.PlatformRoles["owner"] = access.PlatformAdmin
		s.PlatformRoles["viewer"] = access.PlatformStaff
		return nil
	}))
	path := "/api/v1/platform/organizations/org/child-limit"
	for _, actor := range []string{"viewer", "org-admin", "team-admin"} {
		for _, method := range []string{"PUT", "DELETE"} {
			require.Equal(t, 403, rosterRequest(g, actor, method, path, `{"max_children":3}`).Code)
		}
	}
	require.Equal(t, 200, rosterRequest(g, "viewer", "GET", path, "").Code)
	for _, body := range []string{`{}`, `{"max_children":null}`, `{"max_children":-1}`, `{"max_children":11}`, `{"max_children":1.5}`, `{"max_children":3,"id":"changed"}`} {
		require.Equal(t, 400, rosterRequest(g, "owner", "PUT", path, body).Code)
	}
	require.Equal(t, 404, rosterRequest(g, "owner", "PUT", "/api/v1/platform/organizations/missing/child-limit", `{"max_children":3}`).Code)
	require.Equal(t, 400, rosterRequest(g, "owner", "PUT", "/api/v1/platform/organizations/"+child+"/child-limit", `{"max_children":3}`).Code)
	response := rosterRequest(g, "owner", "PUT", path, `{"max_children":0}`)
	require.Equal(t, 200, response.Code)
	require.Contains(t, response.Body.String(), `"override":0`)
	require.Equal(t, 400, rosterRequest(g, "owner", "POST", "/api/v1/studio/orgs", `{"name":"Blocked","parent_id":"org"}`).Code)
	require.Equal(t, 200, rosterRequest(g, "owner", "PUT", "/api/v1/platform/organization-settings", `{"max_children":5}`).Code)
	state, _, err := g.Directory.Repo.Read(t.Context())
	require.NoError(t, err)
	require.Equal(t, 0, access.OrganizationChildLimit(state, "org"))
	require.Equal(t, 5, access.OrganizationChildLimit(state, other))
	hierarchy, err := access.Hierarchy(state, "owner", child)
	require.NoError(t, err)
	require.Equal(t, 0, hierarchy.MaxChildren)
	require.Len(t, hierarchy.Children, 1)
	options := rosterRequest(g, "owner", "GET", "/api/v1/studio/orgs/creation?organization_id="+child, "")
	require.Contains(t, options.Body.String(), `"max_children":0`)
	listing := rosterRequest(g, "owner", "GET", "/api/v1/platform/organizations", "")
	var page struct {
		Items []struct {
			ID         string                          `json:"id"`
			ChildCount int                             `json:"child_count"`
			Policy     *access.OrganizationChildPolicy `json:"child_limit"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(listing.Body.Bytes(), &page))
	for _, item := range page.Items {
		if item.ID == "org" {
			require.Equal(t, 1, item.ChildCount)
			require.Equal(t, 0, item.Policy.MaxChildren)
			require.Equal(t, 0, *item.Policy.Override)
		}
		if item.ID == other {
			require.Equal(t, 5, item.Policy.MaxChildren)
			require.Nil(t, item.Policy.Override)
		}
		if item.ID == child {
			require.Nil(t, item.Policy)
		}
	}
	response = rosterRequest(g, "owner", "DELETE", path, "")
	require.Equal(t, 200, response.Code)
	require.Contains(t, response.Body.String(), `"override":null`)
	require.Contains(t, response.Body.String(), `"max_children":5`)
	state, _, err = g.Directory.Repo.Read(t.Context())
	require.NoError(t, err)
	require.NotContains(t, state.ChildOrganizationOverrides, "org")
	last := state.PlatformEvents[len(state.PlatformEvents)-1]
	require.Equal(t, "organization.child_limit.override.reset", last.Action)
	require.Equal(t, "org", last.Target)
	require.Equal(t, "owner", last.Actor)
	require.Contains(t, last.Reason, "0 (organization override) to 5 (platform default)")
	require.Equal(t, 201, rosterRequest(g, "owner", "POST", "/api/v1/studio/orgs", `{"name":"Allowed after reset","parent_id":"org"}`).Code)
}
