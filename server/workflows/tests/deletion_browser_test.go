package workflows_test

import (
	"context"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/example/runloom/workflows/internal/api"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/stretchr/testify/require"
)

// Explicit local browser fixture: anonymous, synthetic data, loopback only.
// The browser suite shuts this down through /__test__/stop; it also expires.
func TestDeletionBrowserAPI(t *testing.T) {
	if os.Getenv("RUNLOOM_DELETION_BROWSER") != "1" {
		t.Skip("opt-in browser fixture")
	}
	b, err := store.New(t.TempDir())
	require.NoError(t, err)
	ctx := context.Background()
	_, err = b.CreateProject(ctx, "Empty fixture")
	require.NoError(t, err)
	full, err := b.CreateProject(ctx, "Full fixture")
	require.NoError(t, err)
	for _, id := range []string{"draft", "published", "editable", "race", "assigned"} {
		value := deletionDefinition(id)
		value.Name = id
		_, err = b.SaveDraft(value)
		require.NoError(t, err)
	}
	require.NoError(t, b.AssignProject(ctx, "assigned", full.ID))
	_, err = b.Publish("published")
	require.NoError(t, err)
	app := api.New(b, nil, "browser-fixture")
	defer app.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:17395")
	require.NoError(t, err)
	stop := make(chan struct{})
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("POST /__test__/stop", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
		once.Do(func() { close(stop) })
	})
	mux.Handle("/", app.Handler())
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	go server.Serve(listener)
	t.Log("Deletion browser fixture ready on loopback port 17395")
	select {
	case <-stop:
	case <-time.After(3 * time.Minute):
		t.Fatal("browser fixture expired")
	}
}
