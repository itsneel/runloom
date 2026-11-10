package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPlatformHealthUsesFixedTargetsAndRedactsResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("must not forward user tokens")
		}
		if r.URL.Path == "/readyz/vault" {
			w.WriteHeader(503)
		}
		_, _ = w.Write([]byte("sensitive upstream configuration"))
	}))
	defer server.Close()
	for _, key := range []string{"RUNLOOM_GATEWAY_URL", "RUNLOOM_WORKFLOWS_URL", "RUNLOOM_WORKER_URL", "RUNLOOM_MANAGEMENT_URL"} {
		t.Setenv(key, server.URL)
	}
	t.Setenv("RUNLOOM_ASSISTANT_URL", "")
	items := platformHealth(t.Context())
	if len(items) != 5 || items[0].Status != "healthy" || items[1].Status != "degraded" || items[2].Status != "degraded" || items[3].Status != "healthy" || items[4].Status != "unknown" {
		t.Fatalf("unexpected statuses: %+v", items)
	}
	raw, _ := json.Marshal(items)
	if strings.Contains(string(raw), "sensitive") || strings.Contains(string(raw), server.URL) {
		t.Fatal("upstream contents or address exposed")
	}
}
