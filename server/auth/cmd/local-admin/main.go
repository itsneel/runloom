package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/example/runloom/auth/internal/access"
	"github.com/example/runloom/auth/internal/config"
	"github.com/example/runloom/auth/internal/localadmin"
)

func main() {
	if err := config.Initialize(); err != nil {
		fail(err.Error())
	}
	reset := flag.Bool("reset-password", false, "Restore all four public local account passwords and end their sessions; preserve platform roles")
	jsonOutput := flag.Bool("json", false, "Print local bootstrap outcome as JSON (public development defaults only)")
	platformSubject := flag.String("platform-admin-subject", "", "Operator-only: bootstrap the first platform administrator from an existing verified user subject; does not create an identity or organization")
	flag.Parse()
	if flag.NArg() != 0 {
		fail("unexpected arguments")
	}
	settings := config.FromEnv()
	if *platformSubject != "" {
		if *reset || *jsonOutput {
			fail("platform bootstrap cannot reset passwords or print local credentials")
		}
		if err := bootstrapPlatform(settings, *platformSubject); err != nil {
			fail(err.Error())
		}
		fmt.Println("Platform administrator provisioned. No organization permissions or credentials changed.")
		return
	}
	base := os.Getenv("RUNLOOM_LOCAL_KEYCLOAK_URL")
	err := localadmin.Validate(os.Getenv("RUNLOOM_ENV"), os.Getenv("RUNLOOM_LOCAL_BOOTSTRAP"), base, settings)
	if prefix := os.Getenv("RUNLOOM_LOCAL_K8S_SERVICE_PREFIX"); prefix != "" {
		err = localadmin.ValidateKubernetes(os.Getenv("RUNLOOM_ENV"), os.Getenv("RUNLOOM_LOCAL_BOOTSTRAP"), base, settings, prefix)
	}
	if err != nil {
		fail(err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	repo, err := access.Open(ctx, settings.DatabaseDriver, settings.DatabaseURL, settings.DatabaseName, settings.DataDir)
	if err != nil {
		fail("cannot open local account directory; check database health")
	}
	defer repo.Close(context.Background())
	repo = access.WithKeycloakOrganizations(repo, &access.KeycloakDirectory{BaseURL: config.Value("RUNLOOM_KEYCLOAK_ADMIN_URL"), Realm: "runloom", ClientID: config.Value("RUNLOOM_KEYCLOAK_PROVISIONER_CLIENT_ID"), SecretFile: config.Value("RUNLOOM_KEYCLOAK_PROVISIONER_SECRET_FILE")})
	user, created, err := localadmin.EnsureIdentity(ctx, base, *reset)
	if err != nil {
		fail(err.Error())
	}
	if err = localadmin.EnsureDirectory(ctx, &access.Directory{Repo: repo}, user); err != nil {
		fail(err.Error())
	}
	member, memberCreated, err := localadmin.EnsureMemberIdentity(ctx, base, *reset)
	if err != nil {
		fail(err.Error())
	}
	if err = localadmin.EnsureMemberDirectory(ctx, &access.Directory{Repo: repo}, member); err != nil {
		fail(err.Error())
	}
	platform, err := localadmin.EnsurePlatformAccounts(ctx, base, &access.Directory{Repo: repo}, *reset)
	if err != nil {
		fail(err.Error())
	}
	if err := localadmin.SeparateOrganizationAdmin(ctx, &access.Directory{Repo: repo}, user); err != nil {
		fail(err.Error())
	}
	if *jsonOutput {
		if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"email": localadmin.Email, "default_password": localadmin.Password, "created": created, "reset": *reset, "organization": "Runloom", "team": "General",
			"member":         map[string]any{"email": localadmin.MemberEmail, "default_password": localadmin.MemberPassword, "created": memberCreated, "reset": *reset, "default_org_role": "member", "default_team_role": "editor"},
			"platform_admin": platform[access.PlatformAdmin], "platform_staff": platform[access.PlatformStaff],
		}); err != nil {
			fail("cannot print local bootstrap outcome")
		}
	} else {
		fmt.Println(localadmin.Result(created, *reset))
		fmt.Println(localadmin.MemberResult(memberCreated, *reset))
		for _, role := range []string{access.PlatformAdmin, access.PlatformStaff} {
			account := platform[role]
			fmt.Printf("Local platform account ready: %s; current role: %q; no tenant memberships added. Existing passwords and platform access are preserved unless an explicit password reset was requested.\n", account.Email, account.Role)
		}
	}
}

func fail(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(1) }
