package services

import (
	"context"
	"errors"
	"fmt"
	"github.com/example/runloom/management/internal/access"
	"github.com/example/runloom/management/internal/api"
	"github.com/example/runloom/management/internal/config"
	observability "github.com/example/runloom/sdk/go-observability"

	"github.com/example/runloom/management/internal/model"
	"github.com/example/runloom/management/internal/store"

	"net/http"
	"time"
)

type Runtime struct {
	Handler http.Handler
	close   func()
}

func (r *Runtime) Close() { r.close() }

func NewRuntime(ctx context.Context, role string) (out *Runtime, err error) {
	if role != "management" {
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
		if role == "auth" && name == "management" {
			continue
		}
		peers[name], err = NewClient(config.Value(key), role, credential)
		if err != nil {
			return nil, err
		}
	}
	directory := remoteDirectory(peers["auth"], nil)
	rawOpen := func(ctx context.Context, team string) (store.Backend, error) {
		return store.OpenTeam(ctx, cfg.DatabaseDriver, cfg.DatabaseURL, cfg.DatabaseName, cfg.DataDir, team)
	}
	pool := store.NewTeamPool(16, rawOpen)
	cleanups = append(cleanups, func() { _ = pool.Close(context.Background()) })
	open := func(ctx context.Context, team string) (store.View, error) {
		own, e := rawOpen(ctx, team)
		if e != nil {
			return nil, e
		}
		b := &Backend{Backend: own, Team: team, Workflows: peers["workflows"]}
		if role == "management" {
			refs, ok := own.(interface {
				ConfigurePolicyReferences(func(string, *int) (model.Definition, error), func(context.Context) (store.ProjectCatalog, error))
			})
			if !ok {
				_ = own.Close(ctx)
				return nil, fmt.Errorf("policy reference adapter unavailable")
			}
			refs.ConfigurePolicyReferences(b.Get, func(ctx context.Context) (v store.ProjectCatalog, e error) {
				e = b.workflow(ctx, "projects/get", struct{}{}, &v)
				return
			})
		}
		return b, nil
	}
	oidc := access.ConfigFromEnv()
	verifier := access.NewOIDC(ctx, oidc)
	gateway := api.NewServiceGateway(directory, verifier, oidc, open)
	cleanups = append(cleanups, gateway.Close)
	internal := &Internal{Trust: trust, OpenTeam: func(r *http.Request, team string) (store.Backend, func(), error) {
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
		c, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		ok := local.Health(c)

		if !ok {
			reply(w, nil, ErrUnavailable)
			return
		}
		reply(w, map[string]any{"service": role, "status": "ok", "database": true, "database_driver": local.Driver(), "temporal": false}, nil)
	}
	mux.HandleFunc("GET /readyz", ready)
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
	telemetry, err := observability.Start(ctx, "management", config.Value("RUNLOOM_TELEMETRY_ENDPOINT"))
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
