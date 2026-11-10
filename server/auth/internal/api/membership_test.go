package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/runloom/auth/internal/access"
	"github.com/stretchr/testify/require"
)

type rosterVerifier map[string]access.User

func (v rosterVerifier) Verify(_ context.Context, token string) (access.User, error) {
	if user, ok := v[token]; ok {
		return user, nil
	}
	return access.User{}, errors.New("invalid test identity")
}

func membershipGateway(t *testing.T) *Gateway {
	t.Helper()
	repo, err := access.Open(t.Context(), "file", "", "", t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repo.Close(context.Background())) })
	directory := &access.Directory{Repo: repo}
	users := rosterVerifier{}
	for _, id := range []string{"owner", "org-admin", "team-admin", "editor", "viewer", "other-team", "outsider", "new-user"} {
		users[id] = access.User{ID: id, Email: id + "@example.test"}
	}
	require.NoError(t, directory.Update(t.Context(), func(s *access.State) error {
		members := map[string]access.Member{}
		team := map[string]access.Member{}
		for id, user := range users {
			s.Users[id] = user
			if id == "outsider" || id == "new-user" {
				continue
			}
			role := "member"
			if id == "owner" {
				role = "owner"
			} else if id == "org-admin" {
				role = "admin"
			}
			members[user.Email] = access.Member{Email: user.Email, UserID: id, Role: role}
			if id == "editor" || id == "viewer" || id == "team-admin" {
				role = id
				if id == "team-admin" {
					role = "admin"
				}
				team[user.Email] = access.Member{Email: user.Email, UserID: id, Role: role}
			}
		}
		// Effective organization access overrides, without duplicating this row.
		team[users["owner"].Email] = access.Member{Email: users["owner"].Email, UserID: "owner", Role: "viewer"}
		s.Orgs["org"] = access.Organization{ID: "org", Name: "Example", Members: members, Teams: map[string]access.Team{
			"team":    {ID: "team", Name: "Engineering", Members: team},
			"private": {ID: "private", Name: "Private", Members: map[string]access.Member{users["other-team"].Email: {Email: users["other-team"].Email, UserID: "other-team", Role: "editor"}}},
		}}
		s.Orgs["other-org"] = access.Organization{ID: "other-org", Name: "Other", Members: map[string]access.Member{
			users["outsider"].Email: {Email: users["outsider"].Email, UserID: "outsider", Role: "member"},
		}, Teams: map[string]access.Team{}}
		return nil
	}))
	gateway := NewServiceGateway(directory, users, access.Config{ClientID: "studio"}, nil)
	t.Cleanup(gateway.Close)
	return gateway
}

func rosterRequest(g *Gateway, actor, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if actor != "" {
		request.Header.Set("Authorization", "Bearer "+actor)
	}
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	g.ServeHTTP(response, request)
	return response
}

func TestTeamRosterReadPermissions(t *testing.T) {
	g := membershipGateway(t)
	path := "/api/v1/studio/orgs/org/teams/team/members"
	for _, actor := range []string{"owner", "org-admin", "team-admin", "editor", "viewer"} {
		t.Run(actor, func(t *testing.T) {
			response := rosterRequest(g, actor, "GET", path, "")
			require.Equal(t, 200, response.Code, response.Body.String())
			var members []struct {
				access.Member
				Inherited bool `json:"inherited"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &members))
			require.Len(t, members, 5)
			emails := []string{}
			for _, member := range members {
				emails = append(emails, member.Email)
				inherited := member.UserID == "owner" || member.UserID == "org-admin"
				require.Equal(t, inherited, member.Inherited)
				if inherited {
					require.Equal(t, "admin", member.Role)
				}
			}
			require.IsIncreasing(t, emails)
		})
	}
	for _, actor := range []string{"other-team", "outsider", "new-user"} {
		require.Equal(t, 403, rosterRequest(g, actor, "GET", path, "").Code)
	}
	require.Equal(t, 401, rosterRequest(g, "", "GET", path, "").Code)
	require.Equal(t, 403, rosterRequest(g, "editor", "GET", "/api/v1/studio/orgs/org/teams/private/members", "").Code)
	require.Equal(t, 403, rosterRequest(g, "editor", "GET", "/api/v1/studio/orgs/other-org/teams/team/members", "").Code)
	require.Equal(t, 403, rosterRequest(g, "editor", "GET", "/api/v1/studio/orgs/org/members", "").Code)
}

func TestRosterVisibilityDoesNotGrantWritePermissions(t *testing.T) {
	g := membershipGateway(t)
	for _, actor := range []string{"editor", "viewer"} {
		path := "/api/v1/studio/orgs/org/teams/team/members"
		require.Equal(t, 403, rosterRequest(g, actor, "POST", path, `{"email":"viewer@example.test","role":"admin"}`).Code)
		require.Equal(t, 403, rosterRequest(g, actor, "DELETE", path+"/viewer@example.test", "").Code)
		require.Equal(t, 403, rosterRequest(g, actor, "POST", "/api/v1/studio/orgs/org/teams", `{"name":"Unauthorized team"}`).Code)
	}
	require.Equal(t, 200, rosterRequest(g, "team-admin", "POST", "/api/v1/studio/orgs/org/teams/team/members", `{"email":"viewer@example.test","role":"editor"}`).Code)
}

func TestOrganizationCreationPermissions(t *testing.T) {
	for _, tc := range []struct {
		actor  string
		status int
	}{
		{"owner", 400}, {"org-admin", 400}, {"new-user", 201},
		{"team-admin", 403}, {"editor", 403}, {"viewer", 403}, {"outsider", 403}, {"", 401},
	} {
		t.Run(tc.actor, func(t *testing.T) {
			g := membershipGateway(t)
			response := rosterRequest(g, tc.actor, "POST", "/api/v1/studio/orgs", `{"name":"Separate organization"}`)
			require.Equal(t, tc.status, response.Code, response.Body.String())
			state, _, err := g.Directory.Repo.Read(t.Context())
			require.NoError(t, err)
			count := 2
			if tc.status == 201 {
				count++
			}
			require.Len(t, state.Orgs, count)
		})
	}
}
