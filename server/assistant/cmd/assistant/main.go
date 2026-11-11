package main

import (
	"context"
	"errors"
	"github.com/example/runloom/assistant/internal/chat"
	"github.com/example/runloom/assistant/internal/config"
	"github.com/example/runloom/assistant/internal/healthcheck"
	auth "github.com/example/runloom/sdk/go-auth"
	observability "github.com/example/runloom/sdk/go-observability"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if handled, code := healthcheck.Handle(os.Args[1:]); handled {
		os.Exit(code)
	}
	if run() != nil {
		log.Fatal("Assistant startup or server failed; check configuration and database availability. Secret values omitted.")
	}
}
func run() error {
	if err := config.Initialize(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	authURL := config.Value("RUNLOOM_AUTH_URL")
	u, err := url.Parse(authURL)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("invalid auth URL")
	}
	var age time.Duration
	if value := config.Value("RUNLOOM_OIDC_MAX_AUTH_AGE"); value != "" {
		age, err = time.ParseDuration(value)
		if err != nil || age <= 0 {
			return errors.New("invalid auth age")
		}
	}
	clientID := config.Value("RUNLOOM_OIDC_CLIENT_ID")
	verifier := auth.New(ctx, auth.Config{Issuer: config.Value("RUNLOOM_OIDC_ISSUER"), JWKSURL: config.Value("RUNLOOM_OIDC_JWKS_URL"), Audience: clientID, ClientID: clientID, Kind: auth.Human, RequiredACR: config.Value("RUNLOOM_OIDC_REQUIRED_ACR"), MaxAuthAge: age})
	provider, err := chat.ConfiguredProvider(config.Value)
	if err != nil {
		return err
	}
	boot, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	store, err := chat.Open(boot, config.Value("RUNLOOM_DATABASE_DRIVER"), config.Value("RUNLOOM_DATABASE_URL"), config.Value("RUNLOOM_DATABASE_NAME"))
	if err != nil {
		return err
	}
	defer store.Close()
	telemetry, err := observability.Start(ctx, "assistant", config.Value("RUNLOOM_TELEMETRY_ENDPOINT"))
	if err != nil {
		return err
	}
	defer telemetry.Close()
	app := &chat.Server{Store: store, Provider: provider, Authorize: chat.Authorization(verifier, authURL), CheckWorkflow: chat.WorkflowAuthorization(config.Value("RUNLOOM_WORKFLOWS_URL")), Authoring: chat.NewAuthoringClient(config.Value("RUNLOOM_WORKFLOWS_URL"))}
	workerDone := make(chan struct{})
	go func() { app.Run(ctx); close(workerDone) }()
	address := config.Value("RUNLOOM_API_ADDRESS")
	if address == "" {
		address = "127.0.0.1:8084"
	}
	server := &http.Server{Addr: address, Handler: telemetry.Wrap(app.Handler()), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 25 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Print("Runloom assistant listening; workflow draft authoring enabled; publishing and execution unavailable")
	err = server.ListenAndServe()
	stop()
	<-workerDone
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
