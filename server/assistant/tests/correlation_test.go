package tests

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/runloom/assistant/internal/chat"
	obs "github.com/example/runloom/sdk/go-observability"
)

func TestWorkflowAndAuthoringClientsPropagateCorrelation(t *testing.T) {
	id := strings.Repeat("b", 32)
	calls := 0
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get(obs.RequestIDHeader) != id || r.Header.Get("Authorization") != "Bearer human" || r.Header.Get("X-Runloom-Team") != "team" {
			t.Error("correlation or existing authorization headers lost")
		}
		if strings.HasSuffix(r.URL.Path, "/authoring") {
			_ = json.NewEncoder(w).Encode(chat.AuthoringContext{RootID: "parent", Revision: strings.Repeat("a", 64), Workflows: json.RawMessage(`[]`)})
		}
	}))
	defer owner.Close()
	telemetry, err := obs.Start(context.Background(), "assistant", "")
	if err != nil {
		t.Fatal(err)
	}
	defer telemetry.Close()
	h := telemetry.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope := chat.Scope{Workflow: "parent", Team: "team"}
		if err := chat.WorkflowAuthorization(owner.URL)(r.Context(), r, scope); err != nil {
			t.Error(err)
		}
		if _, err := chat.NewAuthoringClient(owner.URL).Context(r.Context(), r, scope); err != nil {
			t.Error(err)
		}
	}))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set(obs.RequestIDHeader, id)
	r.Header.Set("Authorization", "Bearer human")
	h.ServeHTTP(httptest.NewRecorder(), r)
	if calls != 2 {
		t.Fatal("both owner clients must be exercised")
	}
}

func TestLumiTelemetryDoesNotBufferSSE(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:4318")
	if err != nil {
		t.Fatal(err)
	}
	collector := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/x-protobuf")
	})}
	go collector.Serve(listener)
	defer collector.Close()
	telemetry, err := obs.Start(context.Background(), "assistant", "http://127.0.0.1:4318")
	if err != nil {
		t.Fatal(err)
	}
	defer telemetry.Close()
	release := make(chan struct{})
	server := httptest.NewServer(telemetry.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: first\n\n")
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Error(err)
		}
		<-release
		_, _ = fmt.Fprint(w, "data: done\n\n")
	})))
	defer server.Close()
	defer close(release)
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if len(response.Header.Get(obs.RequestIDHeader)) != 32 {
		t.Fatal("SSE response missing correlation")
	}
	line, err := bufio.NewReader(response.Body).ReadString('\n')
	if err != nil || line != "data: first\n" {
		t.Fatal("telemetry buffered the stream before handler completion")
	}
}
