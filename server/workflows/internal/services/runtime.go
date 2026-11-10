package services

import (
	"context"
	"errors"
	"fmt"
	observability "github.com/example/runloom/sdk/go-observability"
	"github.com/example/runloom/workflows/internal/access"
	"github.com/example/runloom/workflows/internal/api"
	"github.com/example/runloom/workflows/internal/config"
	"github.com/example/runloom/workflows/internal/consumer"
	"github.com/example/runloom/workflows/internal/protection"
	"github.com/example/runloom/workflows/internal/tenancy"

	"github.com/example/runloom/workflows/internal/store"
	"go.temporal.io/sdk/client"
	"net/http"
	"time"
)

type Runtime struct {
	Handler http.Handler
	close   func()
}

func (r *Runtime) Close() { r.close() }

// WorkerBackend is used only by activity-side adapters, never workflow code.
func WorkerBackend(local store.Backend, team string) (store.Backend, error) {
	if config.Value("RUNLOOM_MANAGEMENT_URL") == "" {
		return local, nil
	}
	m, err := NewClient(config.Value("RUNLOOM_MANAGEMENT_URL"), "worker", config.Value("RUNLOOM_SERVICE_TOKEN_FILE"))
	if err != nil {
		return nil, err
	}
	return &Backend{Backend: local, Role: "worker", Team: team, Management: m}, nil
}

func NewRuntime(ctx context.Context, role string) (out *Runtime, err error) {
	if role != "workflows" {
		return nil, fmt.Errorf("invalid service role")
	}
	cfg := config.FromEnv()
	cleanups := []func(){}
	defer func() {
		if err != nil {
			for i := len(cleanups) - 1; i >= 0; i-- {
				cleanups[i]()
			}
		}
	}()
	local, err := store.Open(ctx, cfg.DatabaseDriver, cfg.DatabaseURL, cfg.DatabaseName, cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("service database unavailable")
	}
	cleanups = append(cleanups, func() { _ = local.Close(context.Background()) })
	trust, err := LoadTrust(config.Value("RUNLOOM_SERVICE_TRUST_FILE"))
	if err != nil {
		return nil, err
	}
	credential := config.Value("RUNLOOM_SERVICE_TOKEN_FILE")
	peers := map[string]*Client{}
	for name, key := range map[string]string{"auth": "RUNLOOM_AUTH_URL", "workflows": "RUNLOOM_WORKFLOWS_URL", "management": "RUNLOOM_MANAGEMENT_URL"} {
		if name == role {
			continue
		}
		// Auth reads published grant references and vault audit metadata from Workflows.

		peers[name], err = NewClient(config.Value(key), role, credential)
		if err != nil {
			return nil, err
		}
	}
	repo, e := access.Open(ctx, cfg.DatabaseDriver, cfg.DatabaseURL, cfg.DatabaseName, cfg.DataDir)
	if e != nil {
		return nil, fmt.Errorf("vault database unavailable")
	}
	cleanups = append(cleanups, func() { _ = repo.Close(context.Background()) })
	directory := remoteDirectory(peers["auth"], repo)
	var temporal client.Client
	if role == "workflows" {
		options, e := protection.TemporalOptions(cfg.TemporalAddress, cfg.TemporalNamespace)
		if e != nil {
			return nil, e
		}
		temporal, err = client.Dial(options)
		if err != nil {
			return nil, fmt.Errorf("Temporal unavailable")
		}
		cleanups = append(cleanups, temporal.Close)
	}
	rawOpen := func(ctx context.Context, team string) (store.Backend, error) {
		return store.OpenTeam(ctx, cfg.DatabaseDriver, cfg.DatabaseURL, cfg.DatabaseName, cfg.DataDir, team)
	}
	pool := store.NewTeamPool(16, rawOpen)
	cleanups = append(cleanups, func() { _ = pool.Close(context.Background()) })
	open := func(ctx context.Context, team string) (store.Backend, error) {
		own, e := rawOpen(ctx, team)
		if e != nil {
			return nil, e
		}
		b := &Backend{Backend: own, Role: role, Team: team, Workflows: peers["workflows"], Management: peers["management"]}
		return b, nil
	}
	oidc := access.ConfigFromEnv()
	verifier := access.NewOIDC(ctx, oidc)
	namespaces, err := tenancy.New(temporal, cfg)
	if err != nil {
		return nil, err
	}
	cleanups = append(cleanups, namespaces.Close)
	base := api.New(local, nil, cfg.TaskQueue)
	gateway := api.NewServiceGateway(role, base, directory, verifier, oidc, open, temporal, cfg.TaskQueue, namespaces)
	cleanups = append(cleanups, gateway.Close)
	if role == "workflows" {
		gateway.Secrets, err = access.NewSecretVault(directory, config.Value("RUNLOOM_SECRETS_KEY_FILE"))
		if err != nil {
			return nil, err
		}
		ledger, e := consumer.Open(ctx, cfg.DatabaseDriver, cfg.DatabaseURL, cfg.DatabaseName, cfg.DataDir)
		if e != nil {
			return nil, fmt.Errorf("execution admission storage unavailable")
		}
		gateway.ConsumerStore = ledger
		cleanups = append(cleanups, func() { _ = ledger.Close(context.Background()) })
	}
	audience := config.Value("RUNLOOM_EXECUTION_AUDIENCE")
	if audience == "" {
		audience = "runloom-execution"
	}
	if audience == oidc.ClientID {
		return nil, fmt.Errorf("execution and Studio audiences must differ")
	}
	gateway.ConsumerVerifier = consumer.NewVerifier(ctx, oidc.Issuer, oidc.JWKSURL, audience)
	gateway.TemporalNamespace = cfg.TemporalNamespace
	internal := &Internal{Role: role, Trust: trust, Directory: directory, Verifier: verifier, OpenTeam: func(r *http.Request, team string) (store.Backend, func(), error) {
		b, e := pool.Acquire(r.Context(), team)
		return b, func() {
			if b != nil {
				_ = b.Close(context.Background())
			}
		}, e
	}}
	mux := http.NewServeMux()
	mux.Handle("/internal/v1/", internal)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]string{"service": role, "status": "ok"}, nil)
	})
	ready := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/readyz/vault" && gateway.Secrets == nil {
			reply(w, nil, ErrUnavailable)
			return
		}
		c, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		ok := local.Health(c)
		if temporal != nil {
			_, e := temporal.CheckHealth(c, &client.CheckHealthRequest{})
			ok = ok && e == nil
		}
		if !ok {
			reply(w, nil, ErrUnavailable)
			return
		}
		reply(w, map[string]any{"service": role, "status": "ok", "database": true, "database_driver": local.Driver(), "temporal": temporal != nil}, nil)
	}
	mux.HandleFunc("GET /readyz", ready)
	if role == "workflows" {
		mux.HandleFunc("GET /readyz/vault", ready)
	}
	mux.HandleFunc("GET /api/v1/health", ready)
	mux.Handle("/", gateway)
	out = &Runtime{Handler: mux, close: func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}}
	return out, nil
}

func Serve(ctx context.Context, handler http.Handler) error {
	telemetry, err := observability.Start(ctx, "workflows", config.Value("RUNLOOM_TELEMETRY_ENDPOINT"))
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
