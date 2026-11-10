package main

import (
	"context"
	observability "github.com/example/runloom/sdk/go-observability"
	"github.com/example/runloom/workflows/internal/healthcheck"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/runloom/workflows/internal/access"
	"github.com/example/runloom/workflows/internal/activities"
	"github.com/example/runloom/workflows/internal/config"
	"github.com/example/runloom/workflows/internal/dbnode"
	"github.com/example/runloom/workflows/internal/engine"
	"github.com/example/runloom/workflows/internal/mailnode"
	"github.com/example/runloom/workflows/internal/protection"
	"github.com/example/runloom/workflows/internal/services"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/example/runloom/workflows/internal/tenancy"
	"github.com/example/runloom/workflows/internal/usage"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

func main() {
	if handled, code := healthcheck.Handle(os.Args[1:]); handled {
		os.Exit(code)
	}
	if err := config.Initialize(); err != nil {
		log.Fatal(err)
	}
	settings := config.FromEnv()
	telemetry, telemetryErr := observability.Start(context.Background(), "worker", config.Value("RUNLOOM_TELEMETRY_ENDPOINT"))
	if telemetryErr != nil {
		log.Fatal("invalid telemetry configuration")
	}
	defer telemetry.Close()
	options, err := protection.TemporalOptions(settings.TemporalAddress, settings.TemporalNamespace)
	if err != nil {
		log.Fatal("execution encryption configuration invalid")
	}
	temporalClient, err := client.Dial(options)
	if err != nil {
		log.Fatal("connect to Temporal failed; details withheld")
	}
	defer temporalClient.Close()
	databaseCtx, databaseCancel := context.WithTimeout(context.Background(), 10*time.Second)
	database, err := store.Open(databaseCtx, settings.DatabaseDriver, settings.DatabaseURL, settings.DatabaseName, settings.DataDir)
	databaseCancel()
	if err != nil {
		log.Fatal("connect to workflow database failed; details withheld")
	}
	defer database.Close(context.Background())

	directoryRepo, err := access.Open(context.Background(), settings.DatabaseDriver, settings.DatabaseURL, settings.DatabaseName, settings.DataDir)
	if err != nil {
		log.Fatal("open workflow secret storage failed")
	}
	defer directoryRepo.Close(context.Background())
	vault, err := access.NewSecretVault(&access.Directory{Repo: directoryRepo}, config.Value("RUNLOOM_SECRETS_KEY_FILE"))
	if err != nil {
		log.Fatal(err)
	}
	httpActivity := &activities.HTTP{Secrets: vault}
	databaseActivity := &activities.Database{Vault: vault, Executor: dbnode.Configured()}
	emailConfig, err := mailnode.FromEnv()
	if err != nil {
		log.Fatal("invalid email configuration: ", err)
	}
	emailActivity := &activities.Email{Sender: &mailnode.Sender{Config: emailConfig}}
	prices, err := usage.Load(config.Value("RUNLOOM_LLM_PRICING_FILE"))
	if err != nil {
		log.Fatal(err)
	}
	provider := config.Value("RUNLOOM_LLM_PROVIDER")
	if provider == "" {
		provider = "openai-compatible"
	}
	if len(provider) > 100 {
		log.Fatal("LLM provider identifier exceeds 100 bytes")
	}
	teamStores := store.NewTeamPool(16, func(ctx context.Context, team string) (store.Backend, error) {
		b, err := store.OpenTeam(ctx, settings.DatabaseDriver, settings.DatabaseURL, settings.DatabaseName, settings.DataDir, team)
		if err != nil {
			return nil, err
		}
		wrapped, err := services.WorkerBackend(b, team)
		if err != nil {
			_ = b.Close(ctx)
		}
		return wrapped, err
	})
	defer teamStores.Close(context.Background())
	recorder := &activities.RunRecorder{OpenTeam: func(ctx context.Context, team string) (activities.RecordingStore, error) {
		return teamStores.Acquire(ctx, team)
	}}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := tenancy.New(temporalClient, settings)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	fleet := &tenancy.Workers{Pool: pool, Build: func(c client.Client) worker.Worker {
		w := worker.New(c, settings.TaskQueue, worker.Options{WorkerStopTimeout: 30 * time.Second})
		meteredLLM := &activities.MeteredLLM{Prices: prices, Provider: provider, Ancestors: activities.LLMAncestors(c), OpenTeam: func(ctx context.Context, team string) (activities.MeteringStore, error) {
			return teamStores.Acquire(ctx, team)
		}}
		w.RegisterActivityWithOptions(meteredLLM.Call, activity.RegisterOptions{Name: "runloom.llm"})
		w.RegisterActivityWithOptions(meteredLLM.Check, activity.RegisterOptions{Name: "runloom.internal.check_llm_budget"})
		w.RegisterActivityWithOptions(recorder.Record, activity.RegisterOptions{Name: "runloom.internal.record_run"})
		w.RegisterActivityWithOptions(emailActivity.Send, activity.RegisterOptions{Name: "runloom.internal.email"})
		w.RegisterActivityWithOptions(activities.Code, activity.RegisterOptions{Name: "runloom.internal.code"})
		w.RegisterActivityWithOptions(activities.ValidateOutput, activity.RegisterOptions{Name: "runloom.internal.validate_output"})
		w.RegisterActivityWithOptions(httpActivity.Call, activity.RegisterOptions{Name: "runloom.internal.http"})
		w.RegisterActivityWithOptions(httpActivity.Integration, activity.RegisterOptions{Name: "runloom.internal.integration"})
		w.RegisterActivityWithOptions(databaseActivity.Execute, activity.RegisterOptions{Name: "runloom.internal.database"})
		w.RegisterWorkflowWithOptions(engine.RunloomWorkflowWithSignalHandlers, workflow.RegisterOptions{Name: "RunloomWorkflowWithSignalHandlers"})
		w.RegisterWorkflowWithOptions(engine.RunloomChildTestWorkflowWithSignalHandlers, workflow.RegisterOptions{Name: "RunloomChildTestWorkflowWithSignalHandlers"})
		w.RegisterWorkflowWithOptions(engine.RunloomDraftTestWorkflowWithSignalHandlers, workflow.RegisterOptions{Name: "RunloomDraftTestWorkflowWithSignalHandlers"})
		w.RegisterWorkflowWithOptions(engine.RunloomWorkflow, workflow.RegisterOptions{Name: "RunloomWorkflow"})
		w.RegisterWorkflowWithOptions(engine.RunloomChildTestWorkflow, workflow.RegisterOptions{Name: "RunloomChildTestWorkflow"})
		w.RegisterWorkflowWithOptions(engine.RunloomDraftTestWorkflow, workflow.RegisterOptions{Name: "RunloomDraftTestWorkflow"})
		w.RegisterActivityWithOptions(activities.Echo, activity.RegisterOptions{Name: "runloom.echo"})
		w.RegisterActivityWithOptions(activities.Compose, activity.RegisterOptions{Name: "runloom.compose"})
		w.RegisterActivityWithOptions(activities.Sum, activity.RegisterOptions{Name: "runloom.sum"})
		return w
	}}
	fleetDone := make(chan struct{})
	go func() { defer close(fleetDone); fleet.Run(ctx) }()
	defer func() { stop(); <-fleetDone }()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	ready := func(w http.ResponseWriter, r *http.Request) {
		if !fleet.Ready() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path == "/readyz/vault" && vault == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		checkCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if _, err := temporalClient.CheckHealth(checkCtx, &client.CheckHealthRequest{}); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if !database.Health(checkCtx) {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
	mux.HandleFunc("GET /readyz", ready)
	mux.HandleFunc("GET /readyz/vault", ready)
	address := config.Value("RUNLOOM_WORKER_HEALTH_ADDRESS")
	if address == "" {
		address = "127.0.0.1:8081"
	}
	server := &http.Server{Addr: address, Handler: telemetry.Wrap(mux), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("worker health server: %v", err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}
