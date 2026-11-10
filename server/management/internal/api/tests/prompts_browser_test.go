package api_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/example/runloom/management/internal/api"
	"github.com/example/runloom/management/internal/services"
	"github.com/example/runloom/management/internal/store"
)

// Opt-in browser fixture: real Management prompt handlers and disposable storage.
// Authentication is deliberately outside this fixture; it never uses cluster data.
func TestPromptPublishBrowserAPI(t *testing.T) {
	if os.Getenv("RUNLOOM_PROMPT_PUBLISH_BROWSER") != "1" {
		t.Skip("opt-in browser fixture")
	}
	repo, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:17291")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("POST /done", func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(done) })
		w.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("/", api.New(&services.Backend{Backend: repo}).Handler())
	server := httptest.NewUnstartedServer(mux)
	server.Listener = listener
	server.Start()
	defer server.Close()
	t.Log("Prompt publish browser fixture ready on 127.0.0.1:17291")
	select {
	case <-done:
	case <-time.After(3 * time.Minute):
		t.Fatal("browser fixture timed out")
	}
}
