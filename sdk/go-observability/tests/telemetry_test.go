package observability_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	obs "github.com/example/runloom/sdk/go-observability"
)

func TestDisabledAndInvalidConfiguration(t *testing.T) {
	tel, err := obs.Start(context.Background(), "gateway", "")
	if err != nil {
		t.Fatal(err)
	}
	defer tel.Close()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	w := httptest.NewRecorder()
	tel.Wrap(h).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 204 || len(w.Header().Get(obs.RequestIDHeader)) != 32 {
		t.Fatal("correlation must work even with collection disabled")
	}
	for _, endpoint := range []string{"http://evil.test:4318", "http://127.0.0.1:4318?token=secret", "https://127.0.0.1:4318"} {
		if _, err := obs.Start(context.Background(), "gateway", endpoint); err == nil {
			t.Fatal("accepted invalid endpoint")
		}
	}
}

func TestOTLPExportsBoundedRoutesWithoutPayloads(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:4318")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var data []byte
	signals := map[string][]byte{}
	paths := map[string]bool{}
	receiver := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		data = append(data, b...)
		signals[r.URL.Path] = append(signals[r.URL.Path], b...)
		paths[r.URL.Path] = true
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(200)
	})}
	go receiver.Serve(listener)
	defer receiver.Close()
	tel, err := obs.Start(context.Background(), "assistant", "http://127.0.0.1:4318")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(202)
		_, _ = w.Write([]byte("secret-output"))
	})
	request := httptest.NewRequest("POST", "/api/v1/runs/private-id?token=secret-query", strings.NewReader("secret-input"))
	request.Header.Set("Authorization", "Bearer secret-token")
	w := httptest.NewRecorder()
	tel.Wrap(mux).ServeHTTP(w, request)
	if w.Code != 202 || len(w.Header().Get("X-Runloom-Request-ID")) != 32 {
		t.Fatal("wrong response")
	}
	tel.Close()
	mu.Lock()
	defer mu.Unlock()
	if !paths["/v1/logs"] || !paths["/v1/metrics"] {
		t.Fatal("missing OTLP signals")
	}
	for _, s := range []string{"private-id", "secret-query", "secret-input", "secret-output", "secret-token"} {
		if strings.Contains(string(data), s) {
			t.Fatalf("sensitive marker exported: %s", s)
		}
	}
	for _, s := range []string{"http.request.completed", "POST /api/v1/runs/{id}", "http.server.request.duration", "runloom.http.requests"} {
		if !strings.Contains(string(data), s) {
			t.Fatalf("missing signal %s", s)
		}
	}
	id := w.Header().Get(obs.RequestIDHeader)
	if !strings.Contains(string(signals["/v1/logs"]), id) || strings.Contains(string(signals["/v1/metrics"]), id) {
		t.Fatal("request IDs must appear in logs, never metric dimensions")
	}
	if !strings.Contains(string(signals["/v1/logs"]), "assistant") {
		t.Fatal("Lumi service label missing")
	}
}

func TestCollectorOutageDoesNotBlockRequests(t *testing.T) {
	tel, err := obs.Start(context.Background(), "auth", "http://127.0.0.1:4318")
	if err != nil {
		t.Fatal(err)
	}
	defer tel.Close()
	h := tel.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
	start := time.Now()
	for i := 0; i < 2000; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/unknown/private", nil))
		if w.Code != 401 {
			t.Fatal("status changed")
		}
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("collector outage blocked requests")
	}
}
