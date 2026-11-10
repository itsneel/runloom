package main

import (
	"context"
	"fmt"
	"github.com/example/runloom/auth/internal/access"
	"github.com/example/runloom/auth/internal/config"
	"time"
)

func bootstrapPlatform(settings config.Settings, subject string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repo, err := access.Open(ctx, settings.DatabaseDriver, settings.DatabaseURL, settings.DatabaseName, settings.DataDir)
	if err != nil {
		return fmt.Errorf("cannot open platform identity directory")
	}
	defer repo.Close(context.Background())
	repo = access.WithKeycloakOrganizations(repo, &access.KeycloakDirectory{BaseURL: config.Value("RUNLOOM_KEYCLOAK_ADMIN_URL"), Realm: access.ConfigFromEnv().Realm, ClientID: config.Value("RUNLOOM_KEYCLOAK_PROVISIONER_CLIENT_ID"), SecretFile: config.Value("RUNLOOM_KEYCLOAK_PROVISIONER_SECRET_FILE")})
	s, _, err := repo.ReadOwned(ctx)
	if err != nil {
		return fmt.Errorf("cannot read platform identity directory")
	}
	u, ok := s.Users[subject]
	if !ok {
		return fmt.Errorf("user must sign in with a verified email before provisioning")
	}
	if err = (&access.Directory{Repo: repo}).BootstrapPlatformAdmin(ctx, u); err != nil {
		return err
	}
	s, _, err = repo.ReadOwned(ctx)
	if err != nil {
		return fmt.Errorf("cannot verify platform assignment")
	}
	if s.PlatformRoles[subject] != access.PlatformAdmin {
		return fmt.Errorf("a platform administrator already exists; use the platform console to manage access")
	}
	return nil
}
