package api

import (
	"encoding/json"
	"fmt"
	"github.com/example/runloom/workflows/internal/access"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/stretchr/testify/require"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Opt-in loopback fixture for the actual Angular prompt editor. Authentication
// uses testVerifier; authorization, routes and persistent stores are real.
func TestPromptBrowserAPI(t *testing.T) {
	if os.Getenv("RUNLOOM_PROMPT_BROWSER") != "1" {
		t.Skip("opt-in interactive browser fixture")
	}
	f := newConsumerFixture(t)
	if os.Getenv("RUNLOOM_SECRETS_BROWSER") == "1" {
		keyFile := filepath.Join(t.TempDir(), "key")
		require.NoError(t, os.WriteFile(keyFile, []byte(strings.Repeat("k", 32)), 0600))
		var err error
		f.g.Secrets, err = access.NewSecretVault(f.directory, keyFile)
		require.NoError(t, err)
	}
	tenant, err := f.g.tenant(t.Context(), f.team)
	require.NoError(t, err)
	otherTeam := ""
	if os.Getenv("RUNLOOM_DASHBOARD_BROWSER") == "1" {
		admin := access.User{ID: "admin", Email: "admin@example.test"}
		viewer := access.User{ID: "viewer", Email: "viewer@example.test"}
		_, err = f.directory.Bind(t.Context(), viewer)
		require.NoError(t, err)
		require.NoError(t, f.directory.SetMember(t.Context(), admin, f.org, "", viewer.Email, "member", false))
		require.NoError(t, f.directory.SetMember(t.Context(), admin, f.org, f.team, viewer.Email, "viewer", false))
		f.g.Verifier = testVerifier{"admin": admin, "viewer": viewer}
		otherTeam, err = f.directory.CreateTeam(t.Context(), admin, f.org, "Other team")
		require.NoError(t, err)
		for i := 0; i < 32; i++ {
			started := time.Date(2026, 10, 1, 10, i, 0, 0, time.UTC)
			ended := started.Add(time.Duration(i+1) * time.Second).Format(time.RFC3339Nano)
			status := "completed"
			if i%3 == 0 {
				status = "failed"
			}
			trigger := "manual"
			if i%2 == 0 {
				trigger = "draft_test"
			}
			id := fmt.Sprintf("dashboard-run-%02d", i)
			_, err = tenant.backend.RecordRunStart(store.RunRecord{WorkflowID: id, TemporalRunID: id, DefinitionID: fmt.Sprintf("workflow-%d", i%4), DefinitionVersion: 1, TriggerType: trigger, TemporalStatus: status, CreatedAt: started.Format(time.RFC3339Nano), CompletedAt: &ended, Input: map[string]any{"sample": "Test customer"}, Output: map[string]any{"result": "done"}})
			require.NoError(t, err)
			_, err = tenant.backend.AppendAudit(store.AuditEvent{WorkflowID: id, EventType: "workflow." + status, Actor: "test-user", Payload: map[string]any{"sample": true}})
			require.NoError(t, err)
		}
	}
	_, err = tenant.backend.SaveDraft(model.Definition{SchemaVersion: "1.0", ID: "prompt-test", Name: "Prompt test", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}}, Steps: []model.Step{{ID: "ask", Type: "llm", Model: "default", Prompt: "Hello"}}})
	require.NoError(t, err)
	done := make(chan struct{})
	var once sync.Once
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fixture":
			_ = json.NewEncoder(w).Encode(map[string]string{"team": f.team, "org": f.org, "other_team": otherTeam})
		case "/done":
			once.Do(func() { close(done) })
			w.WriteHeader(204)
		default:
			f.g.ServeHTTP(w, r)
		}
	})
	listener, err := net.Listen("tcp", "127.0.0.1:17290")
	require.NoError(t, err)
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	defer server.Close()
	t.Log("Browser API ready on 127.0.0.1:17290")
	select {
	case <-done:
	case <-time.After(3 * time.Minute):
		t.Fatal("browser fixture timed out")
	}
}
