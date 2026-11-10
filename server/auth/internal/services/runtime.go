package services

import (
	"context"
	"errors"
	"fmt"
	"github.com/example/runloom/auth/internal/access"
	"github.com/example/runloom/auth/internal/api"
	"github.com/example/runloom/auth/internal/config"
	"github.com/example/runloom/auth/internal/consumer"
	"github.com/example/runloom/auth/internal/store"
	observability "github.com/example/runloom/sdk/go-observability"
	"net/http"
	"time"
)

type Runtime struct {
	Handler http.Handler
	close   func()
}

func (r *Runtime) Close() { r.close() }
func Serve(ctx context.Context, handler http.Handler) error {
	telemetry, err := observability.Start(ctx, "auth", config.Value("RUNLOOM_TELEMETRY_ENDPOINT"))
	if err != nil {
		return err
	}
	defer telemetry.Close()
	handler = telemetry.Wrap(handler)
	s := &http.Server{Addr: config.FromEnv().APIAddress, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_ = s.Shutdown(c)
		case <-done:
		}
	}()
	err = s.ListenAndServe()
	close(done)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func NewRuntime(ctx context.Context, role string) (out *Runtime, err error) {
	if role != "auth" {
		return nil, fmt.Errorf("invalid service role")
	}
	cfg := config.FromEnv()
	repo, err := access.Open(ctx, cfg.DatabaseDriver, cfg.DatabaseURL, cfg.DatabaseName, cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("directory database unavailable")
	}
	defer func() {
		if err != nil {
			_ = repo.Close(context.Background())
		}
	}()
	trust, err := LoadTrust(config.Value("RUNLOOM_SERVICE_TRUST_FILE"))
	if err != nil {
		return nil, err
	}
	workflows, err := NewClient(config.Value("RUNLOOM_WORKFLOWS_URL"), "auth", config.Value("RUNLOOM_SERVICE_TOKEN_FILE"))
	if err != nil {
		return nil, err
	}
	oidc := access.ConfigFromEnv()
	metadataRepo := repo
	providerReady := config.Value("RUNLOOM_KEYCLOAK_ADMIN_URL") != "" && config.Value("RUNLOOM_KEYCLOAK_PROVISIONER_SECRET_FILE") != ""
	bootstrapPending := config.Value("RUNLOOM_LOCAL_ORGANIZATION_BOOTSTRAP_PENDING") == "true"
	if !providerReady && !bootstrapPending {
		return nil, fmt.Errorf("Keycloak organization directory provisioning must be configured")
	}
	repo = access.WithKeycloakOrganizations(repo, &access.KeycloakDirectory{BaseURL: config.Value("RUNLOOM_KEYCLOAK_ADMIN_URL"), Realm: oidc.Realm, ClientID: config.Value("RUNLOOM_KEYCLOAK_PROVISIONER_CLIENT_ID"), SecretFile: config.Value("RUNLOOM_KEYCLOAK_PROVISIONER_SECRET_FILE")})
	directory := &access.Directory{Repo: repo}
	verifier := access.NewOIDC(ctx, oidc)
	gateway := api.NewServiceGateway(directory, verifier, oidc, func(ctx context.Context, team string) (store.Backend, error) {
		return &Backend{Team: team, Workflows: workflows}, nil
	})
	gateway.OrganizationEvents = func(ctx context.Context, org string) (events []access.Event, err error) {
		err = workflows.call(ctx, "orgs/"+org+"/secrets/audit", struct{}{}, &events)
		return
	}
	gateway.PlatformHealth = platformHealth
	gateway.PlatformVerifier = access.NewPlatformOIDC(ctx, oidc)
	audience := config.Value("RUNLOOM_EXECUTION_AUDIENCE")
	if audience == "" {
		audience = "runloom-execution"
	}
	if audience == oidc.ClientID {
		return nil, fmt.Errorf("execution and Studio audiences must differ")
	}
	if address := config.Value("RUNLOOM_KEYCLOAK_ADMIN_URL"); address != "" {
		if config.Value("RUNLOOM_KEYCLOAK_PROVISIONER_CLIENT_ID") == "" || config.Value("RUNLOOM_KEYCLOAK_PROVISIONER_SECRET_FILE") == "" {
			return nil, fmt.Errorf("provisioner configuration incomplete")
		}
		gateway.ConsumerProvider = &consumer.Keycloak{BaseURL: address, Realm: oidc.Realm, ClientID: config.Value("RUNLOOM_KEYCLOAK_PROVISIONER_CLIENT_ID"), SecretFile: config.Value("RUNLOOM_KEYCLOAK_PROVISIONER_SECRET_FILE"), Audience: audience}
	}
	mux := http.NewServeMux()
	mux.Handle("/internal/v1/", &Internal{Trust: trust, Directory: directory, Verifier: verifier})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]string{"service": "auth", "status": "ok"}, nil)
	})
	ready := func(w http.ResponseWriter, r *http.Request) {
		c, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		probeRepo := repo
		if !providerReady && bootstrapPending {
			probeRepo = metadataRepo
		} // Local setup provisions Keycloak after the first Helm rollout.
		if _, _, err := probeRepo.Read(c); err != nil {
			reply(w, nil, ErrUnavailable)
			return
		}
		reply(w, map[string]any{"service": "auth", "database": true, "database_driver": cfg.DatabaseDriver}, nil)
	}
	mux.HandleFunc("GET /readyz", ready)
	mux.HandleFunc("GET /api/v1/health", ready)
	mux.Handle("/", gateway)
	return &Runtime{Handler: mux, close: func() { gateway.Close(); _ = repo.Close(context.Background()) }}, nil
}
