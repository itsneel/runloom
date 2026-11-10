package tests

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/runloom/gateway/internal/services"
	obs "github.com/example/runloom/sdk/go-observability"
)

func TestGatewayReplacesPublicIDAndPreservesDownstreamCorrelation(t *testing.T) {
	downstreamID := ""
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downstreamID = r.Header.Get(obs.RequestIDHeader)
		if r.Header.Get("X-Runloom-Service") != "" || r.Header.Get("X-Runloom-Actor") != "" {
			t.Error("gateway forwarded trusted identity headers")
		}
		// A response cannot override the edge's canonical request ID.
		w.Header().Set(obs.RequestIDHeader, "spoofed-response")
		_, _ = io.WriteString(w, downstreamID)
	}))
	defer downstream.Close()
	for _, key := range []string{"RUNLOOM_AUTH_URL", "RUNLOOM_WORKFLOWS_URL", "RUNLOOM_MANAGEMENT_URL", "RUNLOOM_ASSISTANT_URL"} {
		t.Setenv(key, downstream.URL)
	}
	gateway, err := services.NewGateway()
	if err != nil {
		t.Fatal(err)
	}
	telemetry, err := obs.Start(context.Background(), "gateway", "")
	if err != nil {
		t.Fatal(err)
	}
	defer telemetry.Close()
	for _, path := range []string{"/api/v1/studio/me", "/api/v1/studio/prompts", "/api/v1/runs", "/api/v1/studio/assistant/conversations"} {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set(obs.RequestIDHeader, strings.Repeat("a", 32))
		r.Header.Set("X-Runloom-Service", "auth")
		r.Header.Set("X-Runloom-Actor", "admin")
		w := httptest.NewRecorder()
		telemetry.Wrap(gateway).ServeHTTP(w, r)
		id := w.Header().Get(obs.RequestIDHeader)
		if w.Code != 200 || len(w.Header().Values(obs.RequestIDHeader)) != 1 || len(id) != 32 || id == strings.Repeat("a", 32) || id != downstreamID || id != w.Body.String() {
			t.Fatalf("%s: correlation lost or spoofed", path)
		}
	}
}
