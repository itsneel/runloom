package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/example/runloom/assistant/internal/chat"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProvider(t *testing.T) {
	tests := []struct {
		name, body string
		code       int
		wantErr    bool
	}{
		{"success", "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", 200, false},
		{"truncated", "data: {\"choices\":[{\"delta\":{\"content\":\"Partial\"}}]}\n\n", 200, true},
		{"length", "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n", 200, true},
		{"empty", "data: [DONE]\n\n", 200, true},
		{"credentials", "secret-password-must-not-appear", 401, true},
		{"quota", "private-provider-details", 429, true},
		{"malformed", "data: NOT JSON\n\n", 200, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/chat/completions" {
					t.Error("wrong endpoint")
				}
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				if body["stream"] != true || body["tools"] != nil || body["store"] != false {
					t.Error("unsafe provider request")
				}
				w.WriteHeader(test.code)
				_, _ = io.WriteString(w, test.body)
			}))
			defer upstream.Close()
			p, err := chat.NewProvider("openai-compatible", upstream.URL, "test-model", "", true, 128)
			if err != nil {
				t.Fatal(err)
			}
			err = p.Stream(context.Background(), []chat.Message{{Role: "user", Content: "hello"}}, func(string) error { return nil })
			if (err != nil) != test.wantErr {
				t.Fatalf("unexpected result: %v", err)
			}
			if err != nil && (strings.Contains(err.Error(), "secret-password") || strings.Contains(err.Error(), "private-provider")) {
				t.Fatal("provider response leaked")
			}
		})
	}
	if _, err := chat.NewProvider("openai-compatible", "http://untrusted", "model", "", false, 128); err == nil {
		t.Fatal("plaintext accepted")
	}
	if _, err := chat.NewProvider("openai-compatible", "https://user:password@example.test", "model", "", false, 128); err == nil {
		t.Fatal("URL credentials accepted")
	}
}

func liveStore(t *testing.T) *chat.Store {
	t.Helper()
	uri := os.Getenv("RUNLOOM_TEST_ASSISTANT_URL")
	if uri == "" {
		t.Skip("set RUNLOOM_TEST_ASSISTANT_URL to a disposable database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store, err := chat.Open(ctx, os.Getenv("RUNLOOM_TEST_ASSISTANT_DRIVER"), uri, testDatabaseName())
	if err != nil {
		t.Fatal("test database unavailable")
	}
	t.Cleanup(store.Close)
	return store
}
func testDatabaseName() string {
	if name := os.Getenv("RUNLOOM_TEST_ASSISTANT_NAME"); name != "" {
		return name
	}
	return "runloom_assistant_test"
}
func request(t *testing.T, client *http.Client, method, url, user, body string, status int) chat.Conversation {
	t.Helper()
	r, _ := http.NewRequest(method, url, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+user)
	r.Header.Set("Content-Type", "application/json")
	res, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode != status {
		t.Fatalf("%s: status %d wanted %d; %s", method, res.StatusCode, status, data)
	}
	var c chat.Conversation
	_ = json.Unmarshal(data, &c)
	return c
}
func TestLiveChatWithoutProvider(t *testing.T) {
	store := liveStore(t)
	app := &chat.Server{Store: store, Authorize: func(_ context.Context, r *http.Request, org, team string) (chat.Scope, error) {
		return chat.Scope{User: strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), Org: org, Team: team}, nil
	}}
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	client := server.Client()
	client.Timeout = 5 * time.Second
	root := server.URL + "/api/v1/studio/assistant/conversations"
	scope := "?org_id=org&team_id=team"
	id := uuid.NewString()
	request(t, client, "POST", root+scope, "alice", fmt.Sprintf(`{"id":%q}`, id), 201)
	url := root + "/" + id
	// No worker is running: both messages must be saved in the submission itself.
	for turn := 1; turn <= 2; turn++ {
		body := fmt.Sprintf(`{"request_id":%q,"content":"Hello without a model"}`, uuid.NewString())
		c := request(t, client, "POST", url+"/messages"+scope, "alice", body, 202)
		if c.Active() || len(c.Messages) != turn*2 || len(c.Jobs) != turn {
			t.Fatal("chat did not complete and persist both messages")
		}
		job := c.Jobs[turn-1]
		if job.Status != "completed" || job.Model != "" || job.Error != "" || c.Messages[turn*2-1].Content != "LLM is not configured." {
			t.Fatal("expected configuration notice without a model or job error")
		}
		retry := request(t, client, "POST", url+"/messages"+scope, "alice", body, 200)
		if retry.Revision != c.Revision || len(retry.Messages) != turn*2 {
			t.Fatal("retry duplicated messages")
		}
		request(t, client, "POST", url+"/messages"+scope, "alice", strings.Replace(body, "Hello", "Changed", 1), 409)
	}
	request(t, client, "GET", url+scope, "bob", "", 404)
	request(t, client, "GET", url+"?org_id=org&team_id=other", "alice", "", 404)
	reloaded, err := liveStore(t).Get(context.Background(), id)
	if err != nil || len(reloaded.Messages) != 4 || reloaded.Messages[3].Content != "LLM is not configured." {
		t.Fatal("notice and conversation did not survive reconnecting to storage")
	}
	r, _ := http.NewRequest("GET", url+"/events"+scope, nil)
	r.Header.Set("Authorization", "Bearer alice")
	res, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil || res.StatusCode != 200 || !strings.Contains(string(data), "LLM is not configured.") {
		t.Fatal("saved notice not delivered through the live event endpoint")
	}
}

func TestLivePersistenceJobsAndStreaming(t *testing.T) {
	store := liveStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, word := range []string{"Let's ", "plan ", "your workflow."} {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(350 * time.Millisecond):
			}
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", word)
			w.(http.Flusher).Flush()
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer upstream.Close()
	p, _ := chat.NewProvider("openai-compatible", upstream.URL, "fixture", "", true, 128)
	app := &chat.Server{Store: store, Provider: p, Authorize: func(_ context.Context, r *http.Request, org, team string) (chat.Scope, error) {
		user := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if user == "" {
			return chat.Scope{}, chat.ErrUnauthorized
		}
		if org != "org" || team != "team" {
			return chat.Scope{}, chat.ErrForbidden
		}
		return chat.Scope{User: user, Org: org, Team: team}, nil
	}}
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	// Two worker instances compete against the same database to verify fencing.
	done := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		go func() { app.Run(ctx); done <- struct{}{} }()
	}
	defer func() { cancel(); <-done; <-done }()
	root := server.URL + "/api/v1/studio/assistant/conversations"
	scope := "?org_id=org&team_id=team"
	id := uuid.NewString()
	created := request(t, server.Client(), "POST", root+scope, "alice", fmt.Sprintf("{\"id\":%q}", id), 201)
	if created.Scope.Org != "org" {
		t.Fatal("scope missing")
	}
	request(t, server.Client(), "POST", root+scope, "alice", fmt.Sprintf("{\"id\":%q}", id), 200)
	request(t, server.Client(), "GET", root+"/"+id+scope, "bob", "", 404)
	request(t, server.Client(), "GET", root+"/"+id+"?org_id=other&team_id=team", "alice", "", 403)
	reqID := uuid.NewString()
	body := fmt.Sprintf("{\"request_id\":%q,\"content\":\"Plan onboarding\"}", reqID)
	accepted := request(t, server.Client(), "POST", root+"/"+id+"/messages"+scope, "alice", body, 202)
	if len(accepted.Jobs) != 1 || accepted.Jobs[0].Kind != "planning" {
		t.Fatal("job not created")
	}
	request(t, server.Client(), "POST", root+"/"+id+"/messages"+scope, "alice", body, 200)
	request(t, server.Client(), "POST", root+"/"+id+"/messages"+scope, "alice", strings.Replace(body, "Plan onboarding", "Changed", 1), 409)
	r, _ := http.NewRequest("GET", root+"/"+id+"/events"+scope, nil)
	r.Header.Set("Authorization", "Bearer alice")
	res, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if strings.Count(string(data), "event: snapshot") < 2 || !strings.Contains(string(data), "Let's plan your workflow.") {
		t.Fatal("response did not stream intermediate persisted snapshots")
	}
	final := request(t, server.Client(), "GET", root+"/"+id+scope, "alice", "", 200)
	if final.Jobs[0].Status != "completed" || calls.Load() != 1 {
		t.Fatalf("duplicate execution or incomplete job: %d", calls.Load())
	}
	second, err := chat.Open(context.Background(), os.Getenv("RUNLOOM_TEST_ASSISTANT_DRIVER"), os.Getenv("RUNLOOM_TEST_ASSISTANT_URL"), testDatabaseName())
	if err != nil {
		t.Fatal("cannot reopen store")
	}
	defer second.Close()
	persisted, err := second.Get(context.Background(), id)
	if err != nil || len(persisted.Messages) != 2 || persisted.Messages[1].Content != "Let's plan your workflow." {
		t.Fatal("history did not survive reopening")
	}
	stale := persisted
	persisted.Title = "Updated"
	if second.Save(context.Background(), &persisted) != nil {
		t.Fatal("save failed")
	}
	if second.Save(context.Background(), &stale) != chat.ErrConflict {
		t.Fatal("stale write accepted")
	}
	if stale.Revision != final.Revision {
		t.Fatal("failed CAS advanced the caller revision")
	}
	if second.Save(context.Background(), &stale) != chat.ErrConflict {
		t.Fatal("retry overwrote a newer revision")
	}
	// A queued job cancelled before execution never calls the provider.
	cancel()
	<-done
	<-done
	// replace channel accounting for deferred cleanup
	done <- struct{}{}
	done <- struct{}{}
	next := request(t, server.Client(), "POST", root+"/"+id+"/messages"+scope, "alice", fmt.Sprintf("{\"request_id\":%q,\"content\":\"Another idea\"}", uuid.NewString()), 202)
	request(t, server.Client(), "POST", root+"/"+id+"/jobs/"+next.Jobs[1].ID+"/cancel"+scope, "alice", "{}", 200)
	saved, _ := store.Get(context.Background(), id)
	if saved.Jobs[1].Status != "cancelled" {
		t.Fatal("cancel not persisted")
	}
	// Simulate a process lost mid-response; expiry must interrupt, not re-bill.
	saved.Jobs[1].Status = "running"
	saved.Jobs[1].Deadline = time.Now().Add(-time.Minute)
	if store.Save(context.Background(), &saved) != nil {
		t.Fatal("seed failed")
	}
	recovery, recoverCancel := context.WithCancel(context.Background())
	recovered := make(chan struct{})
	go func() { app.Run(recovery); close(recovered) }()
	time.Sleep(600 * time.Millisecond)
	recoverCancel()
	<-recovered
	saved, _ = store.Get(context.Background(), id)
	if saved.Jobs[1].Status != "interrupted" || calls.Load() != 1 {
		t.Fatal("abandoned job was retried")
	}
}
