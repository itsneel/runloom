package services

import observability "github.com/example/runloom/sdk/go-observability"
import (
	"context"
	"encoding/json"
	"errors"
	"github.com/example/runloom/gateway/internal/config"
	"net/http"
	"time"
)

var ErrUnavailable = errors.New("dependent service unavailable")

func reply(w http.ResponseWriter, v any, e error) {
	w.Header().Set("Content-Type", "application/json")
	if e != nil {
		w.WriteHeader(503)
		v = map[string]string{"error": "dependent service unavailable"}
	}
	_ = json.NewEncoder(w).Encode(v)
}
func Serve(ctx context.Context, handler http.Handler) error {
	telemetry, err := observability.Start(ctx, "gateway", config.Value("RUNLOOM_TELEMETRY_ENDPOINT"))
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
