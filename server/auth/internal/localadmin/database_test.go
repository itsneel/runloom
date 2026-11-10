package localadmin

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/example/runloom/auth/internal/access"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestDatabaseBootstrap(t *testing.T) {
	for _, driver := range []string{"postgres", "mongodb"} {
		t.Run(driver, func(t *testing.T) {
			variable := "RUNLOOM_TEST_POSTGRES_URL"
			if driver == "mongodb" {
				variable = "RUNLOOM_TEST_MONGODB_URL"
			}
			address := os.Getenv(variable)
			if address == "" {
				t.Skip("local integration database URL not configured")
			}
			ctx := context.Background()
			name := "runloom_local_admin_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			if driver == "postgres" {
				db, err := sql.Open("pgx", address)
				require.NoError(t, err)
				_, err = db.ExecContext(ctx, `CREATE SCHEMA "`+name+`"`)
				require.NoError(t, err)
				t.Cleanup(func() {
					_, err := db.ExecContext(ctx, `DROP SCHEMA "`+name+`" CASCADE`)
					require.NoError(t, err)
					require.NoError(t, db.Close())
				})
				u, err := url.Parse(address)
				require.NoError(t, err)
				q := u.Query()
				q.Set("search_path", name)
				u.RawQuery = q.Encode()
				address = u.String()
			} else {
				client, err := mongo.Connect(options.Client().ApplyURI(address))
				require.NoError(t, err)
				t.Cleanup(func() {
					require.NoError(t, client.Database(name).Drop(ctx))
					require.NoError(t, client.Disconnect(ctx))
				})
			}
			repositories := make([]access.Repository, 2)
			for i := range repositories {
				r, err := access.Open(ctx, driver, address, name, "")
				require.NoError(t, err)
				repositories[i] = r
				t.Cleanup(func() { require.NoError(t, r.Close(ctx)) })
			}
			user := access.User{ID: "local-subject", Email: Email, Name: "Runloom Local Admin"}
			var wg sync.WaitGroup
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					require.NoError(t, EnsureDirectory(ctx, &access.Directory{Repo: repositories[i%2]}, user))
				}(i)
			}
			wg.Wait()
			s, _, err := repositories[0].Read(ctx)
			require.NoError(t, err)
			require.Len(t, s.Orgs, 1)
			require.Len(t, s.Events, 1)
			require.Empty(t, s.PlatformRoles)
			directory := &access.Directory{Repo: repositories[0]}
			// The hosted first-admin operator path remains separate from local defaults.
			require.NoError(t, directory.BootstrapPlatformAdmin(ctx, user))
			s, _, err = repositories[0].Read(ctx)
			require.NoError(t, err)
			require.Equal(t, access.PlatformAdmin, s.PlatformRoles[user.ID])
			require.Len(t, s.PlatformEvents, 1)
			staff := access.User{ID: "staff-subject", Email: "staff@example.test"}
			_, err = directory.Bind(ctx, staff)
			require.NoError(t, err)
			require.NoError(t, directory.SetPlatformRole(ctx, user, staff.Email, access.PlatformStaff, "Database contract test"))
			s, _, err = repositories[1].ReadOwned(ctx)
			require.NoError(t, err)
			require.Equal(t, access.PlatformStaff, s.PlatformRoles[staff.ID])
			require.ErrorIs(t, directory.SetPlatformRole(ctx, staff, staff.Email, access.PlatformAdmin, "Forbidden promotion"), access.ErrForbidden)
			require.Error(t, directory.SetPlatformRole(ctx, user, user.Email, "", "Remove last admin"))
			require.NoError(t, directory.SetPlatformRole(ctx, user, staff.Email, "", "Revoke staff access"))
			s, _, err = repositories[1].ReadOwned(ctx)
			require.NoError(t, err)
			require.Empty(t, s.PlatformRoles[staff.ID])
			require.Len(t, s.PlatformEvents, 3)
			// Two replicas cannot concurrently remove both remaining administrators.
			require.NoError(t, directory.SetPlatformRole(ctx, user, staff.Email, access.PlatformAdmin, "Concurrency test"))
			for i, actor := range []access.User{user, staff} {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_ = (&access.Directory{Repo: repositories[i]}).SetPlatformRole(ctx, actor, actor.Email, "", "Concurrent self removal")
				}()
			}
			wg.Wait()
			s, _, err = repositories[1].ReadOwned(ctx)
			require.NoError(t, err)
			admins := 0
			for _, role := range s.PlatformRoles {
				if role == access.PlatformAdmin {
					admins++
				}
			}
			require.Equal(t, 1, admins)
			for id, org := range s.Orgs {
				require.Equal(t, "owner", access.OrgRole(s, id, user.ID))
				require.Len(t, org.Teams, 1)
				for tid := range org.Teams {
					_, role := access.TeamRole(s, tid, user.ID)
					require.Equal(t, "admin", role)
				}
			}
			exerciseOrganizationAdminSeparation(t, directory, user)
			// Separate local-account contract on a clean, disposable identity state.
			require.NoError(t, directory.Update(ctx, func(s *access.State) error {
				s.PlatformRoles = map[string]string{}
				s.PlatformEvents = nil
				return nil
			}))
			exercisePlatformAccounts(t, directory, &access.Directory{Repo: repositories[1]})
		})
	}
}
