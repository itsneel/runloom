package access

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// Opt-in local identity contract. Creates isolated users and organizations,
// then deletes only those test records. Application databases are disposable.
func TestKeycloakOrganizationDirectoryLive(t *testing.T) {
	address := os.Getenv("RUNLOOM_TEST_KEYCLOAK_URL")
	secret := os.Getenv("RUNLOOM_TEST_KEYCLOAK_SECRET_FILE")
	if address == "" || secret == "" {
		t.Skip("local Keycloak directory credentials required")
	}
	parsed, err := url.Parse(address)
	require.NoError(t, err)
	require.Contains(t, []string{"localhost", "127.0.0.1"}, parsed.Hostname())
	k := &KeycloakDirectory{BaseURL: address, Realm: "runloom", ClientID: "runloom-provisioner", SecretFile: secret}
	for _, driver := range []string{"file", "postgres", "mongodb"} {
		t.Run(driver, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
			defer cancel()
			suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
			prefix := "Runloom identity test " + suffix[:10]
			var base Repository
			switch driver {
			case "file":
				base, err = Open(ctx, "file", "", "", t.TempDir())
			case "postgres":
				raw := os.Getenv("RUNLOOM_TEST_POSTGRES_URL")
				if raw == "" {
					t.Skip("disposable PostgreSQL URL required")
				}
				admin, e := pgx.Connect(ctx, raw)
				require.NoError(t, e)
				schema := "org_" + suffix
				_, e = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize())
				require.NoError(t, e)
				t.Cleanup(func() {
					_, e := admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
					require.NoError(t, e)
					admin.Close(context.Background())
				})
				dbURL, e := url.Parse(raw)
				require.NoError(t, e)
				query := dbURL.Query()
				query.Set("search_path", schema)
				dbURL.RawQuery = query.Encode()
				base, err = Open(ctx, "postgres", dbURL.String(), "", t.TempDir())
			case "mongodb":
				raw := os.Getenv("RUNLOOM_TEST_MONGODB_URL")
				if raw == "" {
					t.Skip("disposable MongoDB replica set required")
				}
				base, err = Open(ctx, "mongodb", raw, "org_"+suffix, t.TempDir())
			}
			require.NoError(t, err)
			defer base.Close(context.Background())
			if driver == "mongodb" {
				defer base.(*repository).mongo.Database("org_" + suffix).Drop(context.Background())
			}
			defer func() {
				native, e := k.organizations(context.Background())
				require.NoError(t, e)
				for _, o := range native {
					if strings.HasPrefix(o.Name, prefix) {
						_, e = k.call(context.Background(), "DELETE", "/organizations/"+o.ID, nil, nil)
						require.NoError(t, e)
					}
				}
			}()
			newUser := func(label string) User {
				email := suffix + "-" + label + "@example.test"
				_, e := k.call(ctx, "POST", "/users", map[string]any{"username": email, "email": email, "enabled": true, "emailVerified": true}, nil)
				require.NoError(t, e)
				var users []nativeMember
				_, e = k.call(ctx, "GET", "/users?exact=true&email="+url.QueryEscape(email), nil, &users)
				require.NoError(t, e)
				require.Len(t, users, 1)
				t.Cleanup(func() {
					_, e := k.call(context.Background(), "DELETE", "/users/"+users[0].ID, nil, nil)
					require.NoError(t, e)
				})
				return User{ID: users[0].ID, Email: users[0].Email}
			}
			owner, member, otherOwner := newUser("owner"), newUser("member"), newUser("other-owner")
			legacy := &Directory{Repo: base}
			_, err = legacy.Bind(ctx, owner)
			require.NoError(t, err)
			root, err := legacy.CreateOrg(ctx, owner, prefix+" root")
			require.NoError(t, err)
			require.NoError(t, legacy.Update(ctx, func(s *State) error { s.PlatformRoles[owner.ID] = PlatformAdmin; return nil }))
			d := &Directory{Repo: WithKeycloakOrganizations(base, k)}
			state, _, err := d.Repo.Read(ctx)
			require.NoError(t, err)
			require.Equal(t, prefix+" root", state.Orgs[root].Name)
			native, err := k.organizations(ctx)
			require.NoError(t, err)
			nativeID := native[root].ID
			raw, _, err := base.ReadOwned(ctx)
			require.NoError(t, err)
			require.Empty(t, raw.Orgs, "Application storage must not retain organization identities")
			require.NotEmpty(t, raw.OrganizationBindings[root].Teams)
			child, err := d.CreateOrganization(ctx, owner, prefix+" child", root)
			require.NoError(t, err)
			var results [2]error
			var wait sync.WaitGroup
			for i := range results {
				wait.Add(1)
				go func(i int) {
					defer wait.Done()
					_, results[i] = d.CreateOrganization(ctx, owner, prefix+" concurrent "+string(rune('a'+i)), root)
				}(i)
			}
			wait.Wait()
			successes := 0
			for _, e := range results {
				if e == nil {
					successes++
				}
			}
			require.Equal(t, 1, successes, "Concurrent requests must not exceed two children")
			_, err = d.CreateOrganization(ctx, owner, prefix+" grandchild", child)
			require.ErrorContains(t, err, "child organizations cannot")
			require.NoError(t, d.SetMember(ctx, owner, child, "", member.Email, "member", false))
			state, err = d.Bind(ctx, member)
			require.NoError(t, err)
			hierarchy, err := Hierarchy(state, member.ID, child)
			require.NoError(t, err)
			require.Equal(t, root, hierarchy.Root.ID)
			require.Len(t, hierarchy.Children, 2)
			require.Empty(t, hierarchy.Root.Role)
			require.Empty(t, OrgRole(state, root, member.ID))
			visible := map[string]bool{}
			for _, org := range View(state, member).Organizations {
				visible[org.ID] = true
			}
			require.True(t, visible[child])
			require.False(t, visible[root], "Hierarchy metadata must not grant parent membership")
			_, err = d.CreateOrganization(ctx, member, prefix+" denied", root)
			require.ErrorIs(t, err, ErrForbidden)
			require.NoError(t, d.RenameOrganization(ctx, owner, root, prefix+" renamed"))
			native, err = k.organizations(ctx)
			require.NoError(t, err)
			require.Equal(t, nativeID, native[root].ID)
			nativeOrg := native[root]
			nativeOrg.Name = prefix + " Keycloak rename"
			_, err = k.call(ctx, "PUT", "/organizations/"+nativeID, nativeOrg, nil)
			require.NoError(t, err)
			state, _, err = d.Repo.Read(ctx)
			require.NoError(t, err)
			require.Equal(t, nativeOrg.Name, state.Orgs[root].Name)
			require.ErrorIs(t, d.SetChildOrganizationLimit(ctx, member, 10), ErrForbidden)
			require.Error(t, d.SetChildOrganizationLimit(ctx, owner, 11))
			require.NoError(t, d.SetChildOrganizationLimit(ctx, owner, 10))
			for i := 2; i < 10; i++ {
				_, err = d.CreateOrganization(ctx, owner, prefix+" extra "+string(rune('a'+i)), root)
				require.NoError(t, err)
			}
			_, err = d.CreateOrganization(ctx, owner, prefix+" eleventh child", root)
			require.ErrorContains(t, err, "limit", "Ten children plus the parent are allowed")
			require.NoError(t, d.SetChildOrganizationLimit(ctx, owner, 1))
			state, _, err = d.Repo.Read(ctx)
			require.NoError(t, err)
			hierarchy, err = Hierarchy(state, owner.ID, root)
			require.NoError(t, err)
			require.Len(t, hierarchy.Children, 10)
			_, err = d.CreateOrganization(ctx, owner, prefix+" blocked", root)
			require.ErrorContains(t, err, "limit")
			manual := Organization{ID: uuid.NewString(), Name: prefix + " administrator override", ParentID: root, Members: map[string]Member{owner.Email: {Email: owner.Email, UserID: owner.ID, Role: "owner"}}}
			require.NoError(t, k.save(ctx, manual, nil), "Direct Keycloak administration can override Runloom policy")
			zero, three := 0, 3
			require.ErrorIs(t, d.SetOrganizationChildLimit(ctx, member, root, &zero), ErrForbidden)
			require.NoError(t, d.SetOrganizationChildLimit(ctx, owner, root, &zero))
			other, e := d.CreateOrganization(ctx, otherOwner, prefix+" independent parent", "")
			require.NoError(t, e)
			_, e = d.CreateOrg(ctx, owner, prefix+" forbidden second root")
			require.ErrorContains(t, e, "one organization family")
			require.ErrorContains(t, d.SetMember(ctx, otherOwner, other, "", owner.Email, "member", false), "one organization family")
			require.ErrorContains(t, d.SetMember(ctx, otherOwner, other, "", member.Email, "member", false), "one organization family")
			require.NoError(t, d.SetOrganizationChildLimit(ctx, owner, other, &three))
			require.NoError(t, d.SetChildOrganizationLimit(ctx, owner, 0))
			for i := 0; i < 3; i++ {
				_, e = d.CreateOrganization(ctx, otherOwner, prefix+" override child "+string(rune('a'+i)), other)
				require.NoError(t, e)
			}
			_, e = d.CreateOrganization(ctx, otherOwner, prefix+" override blocked", other)
			require.ErrorContains(t, e, "limit of 3")
			reloaded, _, e := WithKeycloakOrganizations(base, k).Read(ctx)
			require.NoError(t, e)
			require.Equal(t, 0, OrganizationChildLimit(reloaded, root))
			require.Equal(t, 3, OrganizationChildLimit(reloaded, other))
			require.NoError(t, d.SetOrganizationChildLimit(ctx, owner, other, nil))
			reloaded, _, e = d.Repo.Read(ctx)
			require.NoError(t, e)
			require.Equal(t, 0, OrganizationChildLimit(reloaded, other))
			require.NotContains(t, reloaded.ChildOrganizationOverrides, other)
			hierarchy, e = Hierarchy(reloaded, otherOwner.ID, other)
			require.NoError(t, e)
			require.Len(t, hierarchy.Children, 3, "Lowering/resetting policy preserves existing children")
			raw, _, err = base.ReadOwned(ctx)
			require.NoError(t, err)
			require.Empty(t, raw.Orgs)
			require.Nil(t, raw.IdentityLease)
		})
	}
}
