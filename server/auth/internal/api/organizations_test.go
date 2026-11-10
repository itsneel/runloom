package api

import (
	"encoding/json"
	"github.com/example/runloom/auth/internal/access"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestOrganizationHierarchyAndStableRenameAPI(t *testing.T) {
	g := membershipGateway(t)
	create := func(name, parent string) (int, string) {
		body, _ := json.Marshal(map[string]string{"name": name, "parent_id": parent})
		response := rosterRequest(g, "owner", "POST", "/api/v1/studio/orgs", string(body))
		var out map[string]string
		_ = json.Unmarshal(response.Body.Bytes(), &out)
		return response.Code, out["id"]
	}
	code, child := create("Child one", "org")
	require.Equal(t, 201, code)
	code, _ = create("Child two", "org")
	require.Equal(t, 201, code)
	code, _ = create("Child three", "org")
	require.Equal(t, 400, code)
	code, _ = create("Grandchild", child)
	require.Equal(t, 400, code)
	response := rosterRequest(g, "owner", "GET", "/api/v1/studio/orgs/"+child+"/hierarchy", "")
	require.Equal(t, 200, response.Code)
	require.NotContains(t, response.Body.String(), "@example.test")
	require.NotContains(t, response.Body.String(), "members")
	require.Equal(t, 403, rosterRequest(g, "outsider", "GET", "/api/v1/studio/orgs/"+child+"/hierarchy", "").Code)
	require.Equal(t, 400, rosterRequest(g, "owner", "PATCH", "/api/v1/studio/orgs/"+child, `{"name":"Renamed","id":"changed"}`).Code)
	require.Equal(t, 200, rosterRequest(g, "owner", "PATCH", "/api/v1/studio/orgs/"+child, `{"name":"Renamed"}`).Code)
	state, _, err := g.Directory.Repo.Read(t.Context())
	require.NoError(t, err)
	require.Equal(t, child, state.Orgs[child].ID)
	require.Equal(t, "Renamed", state.Orgs[child].Name)
	require.Equal(t, "org", state.Orgs[child].ParentID)
	event := state.Events[len(state.Events)-1]
	require.Equal(t, "Child one", event.OldName)
	require.Equal(t, "Renamed", event.NewName)
	require.Equal(t, 403, rosterRequest(g, "viewer", "PATCH", "/api/v1/studio/orgs/org", `{"name":"Denied"}`).Code)
}
func TestOrganizationLimitRequiresPlatformAdministrator(t *testing.T) {
	g := membershipGateway(t)
	require.NoError(t, g.Directory.Update(t.Context(), func(s *access.State) error {
		s.PlatformRoles["owner"] = access.PlatformAdmin
		s.PlatformRoles["viewer"] = access.PlatformStaff
		return nil
	}))
	path := "/api/v1/platform/organization-settings"
	for _, actor := range []string{"viewer", "org-admin", "team-admin"} {
		require.Equal(t, 403, rosterRequest(g, actor, "PUT", path, `{"max_children":10}`).Code)
	}
	require.Equal(t, 400, rosterRequest(g, "owner", "PUT", path, `{"max_children":11}`).Code)
	require.Equal(t, 400, rosterRequest(g, "owner", "PUT", path, `{}`).Code)
	require.Equal(t, 400, rosterRequest(g, "owner", "PUT", path, `{"max_children":null}`).Code)
	require.Equal(t, 200, rosterRequest(g, "owner", "PUT", path, `{"max_children":10}`).Code)
	response := rosterRequest(g, "owner", "GET", path, "")
	require.Equal(t, 200, response.Code)
	require.Contains(t, response.Body.String(), `"max_children":10`)
	require.Equal(t, 201, rosterRequest(g, "owner", "POST", "/api/v1/studio/orgs", `{"name":"New child","parent_id":"org"}`).Code)
}

func TestOrganizationCreationOptionsStayWithinSelectedFamily(t *testing.T) {
	g := membershipGateway(t)
	u := access.User{ID: "owner", Email: "owner@example.test"}
	unrelated, err := g.Directory.CreateOrganization(t.Context(), access.User{ID: "new-user", Email: "new-user@example.test"}, "Unrelated parent", "")
	require.NoError(t, err)
	child, err := g.Directory.CreateOrganization(t.Context(), u, "Current child", "org")
	require.NoError(t, err)
	for _, selected := range []string{"org", child} {
		response := rosterRequest(g, "owner", "GET", "/api/v1/studio/orgs/creation?organization_id="+selected, "")
		require.Equal(t, 200, response.Code)
		var options struct {
			Parents       []access.OrganizationSummary `json:"parents"`
			CanCreateRoot bool                         `json:"can_create_root"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &options))
		require.False(t, options.CanCreateRoot)
		require.Len(t, options.Parents, 1)
		require.Equal(t, "org", options.Parents[0].ID)
		require.NotContains(t, response.Body.String(), unrelated)
		hierarchy := rosterRequest(g, "owner", "GET", "/api/v1/studio/orgs/"+selected+"/hierarchy", "")
		require.Equal(t, 200, hierarchy.Code)
		require.NotContains(t, hierarchy.Body.String(), unrelated)
	}
	for _, path := range []string{"/api/v1/studio/orgs/creation", "/api/v1/studio/orgs/creation?organization_id=org"} {
		actor := "owner"
		if path != "/api/v1/studio/orgs/creation" {
			actor = "viewer"
		}
		response := rosterRequest(g, actor, "GET", path, "")
		require.Equal(t, 200, response.Code)
		require.Contains(t, response.Body.String(), `"parents":[]`)
	}
	for _, id := range []string{"missing", unrelated} {
		require.Equal(t, 403, rosterRequest(g, "viewer", "GET", "/api/v1/studio/orgs/creation?organization_id="+id, "").Code)
	}
}

func TestCrossFamilyInvitationsRejected(t *testing.T) {
	g := membershipGateway(t)
	response := rosterRequest(g, "owner", "POST", "/api/v1/studio/orgs/org/members", `{"email":"outsider@example.test","role":"member"}`)
	require.Equal(t, 400, response.Code)
	require.Contains(t, response.Body.String(), "one organization family")
	require.NotContains(t, response.Body.String(), "other-org")
	response = rosterRequest(g, "new-user", "GET", "/api/v1/studio/orgs/creation", "")
	require.Equal(t, 200, response.Code)
	require.Contains(t, response.Body.String(), `"can_create_root":true`)
}
