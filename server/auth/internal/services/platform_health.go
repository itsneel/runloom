package services

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/example/runloom/auth/internal/api"
	"github.com/example/runloom/auth/internal/config"
)

// Targets come only from operator configuration, never from a request. Responses,
// addresses and credentials are not forwarded to the platform console.
func platformHealth(ctx context.Context) []api.ServiceStatus {
	targets := []struct{ name, key, path string }{
		{"gateway", "RUNLOOM_GATEWAY_URL", "/healthz"},
		{"workflows", "RUNLOOM_WORKFLOWS_URL", "/readyz/vault"},
		{"worker", "RUNLOOM_WORKER_URL", "/readyz/vault"},
		{"management", "RUNLOOM_MANAGEMENT_URL", "/readyz"},
		{"assistant", "RUNLOOM_ASSISTANT_URL", "/readyz"},
	}
	items := make([]api.ServiceStatus, len(targets))
	var wg sync.WaitGroup
	for i, target := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			item := api.ServiceStatus{Service: target.name, Status: "unknown", Detail: "Health target not configured"}
			defer func() { items[i] = item }()
			base := config.Value(target.key)
			if base == "" {
				return
			}
			start := time.Now()
			client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(base, "/")+target.path, nil)
			item.Status, item.Detail = "unavailable", "Service did not respond"
			if err != nil {
				return
			}
			response, err := client.Do(req)
			item.LatencyMS = time.Since(start).Milliseconds()
			if err != nil {
				return
			}
			response.Body.Close()
			item.Status, item.Detail = "degraded", "Readiness check failed"
			if response.StatusCode == http.StatusOK {
				item.Status, item.Detail = "healthy", "Readiness check passed"
			}
			if target.name == "gateway" && response.StatusCode == http.StatusOK {
				item.Detail = "Gateway process is responding; dependencies listed separately"
			}
		}()
	}
	wg.Wait()
	return items
}
