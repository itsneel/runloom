package main

import (
	"context"
	"github.com/example/runloom/gateway/internal/config"
	"github.com/example/runloom/gateway/internal/healthcheck"
	"github.com/example/runloom/gateway/internal/services"
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
	handler, err := services.NewGateway()
	if err != nil {
		log.Fatal(err)
	}
	log.Print("Runloom gateway listening")
	if err := services.Serve(ctx, handler); err != nil {
		log.Fatal("gateway stopped")
	}
}
