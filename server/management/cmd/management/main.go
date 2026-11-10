package main

import (
	"context"
	"github.com/example/runloom/management/internal/config"
	"github.com/example/runloom/management/internal/healthcheck"
	"github.com/example/runloom/management/internal/services"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	if handled, code := healthcheck.Handle(os.Args[1:]); handled {
		os.Exit(code)
	}
	if err := config.Initialize(); err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runtime, err := services.NewRuntime(ctx, "management")
	if err != nil {
		log.Fatal(err)
	}
	defer runtime.Close()
	log.Print("Runloom management service listening")
	if err := services.Serve(ctx, runtime.Handler); err != nil {
		log.Fatal("HTTP service stopped")
	}
}
