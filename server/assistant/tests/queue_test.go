package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/runloom/assistant/internal/chat"
	"github.com/google/uuid"
)

func TestLiveDurableQueueAndInterrupt(t *testing.T) {
	store := liveStore(t)
	var mu sync.Mutex
	var prompts [][]chat.Message
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []chat.Message `json:"messages"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid model request")
			return
		}
		mu.Lock()
		prompts = append(prompts, body.Messages)
		call := len(prompts)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		// The first generation must keep streaming despite concurrent queue writes.
		chunks := 3
		if call == 1 {
			chunks = 100
		}
		for i := 0; i < chunks; i++ {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(150 * time.Millisecond):
			}
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"part \"}}]}\n\n")
			w.(http.Flusher).Flush()
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer upstream.Close()
	provider, err := chat.NewProvider("openai-compatible", upstream.URL, "fixture", "", true, 128)
	if err != nil {
		t.Fatal(err)
	}
	app := &chat.Server{Store: store, Provider: provider, Authorize: func(_ context.Context, r *http.Request, org, team string) (chat.Scope, error) {
		return chat.Scope{User: strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), Org: org, Team: team}, nil
	}}
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	client := server.Client()
	client.Timeout = 10 * time.Second
	root := server.URL + "/api/v1/studio/assistant/conversations"
	scope := "?org_id=org&team_id=team"
	id := uuid.NewString()
	request(t, client, "POST", root+scope, "alice", fmt.Sprintf(`{"id":%q}`, id), 201)
	url := root + "/" + id
	submit := func(content, mode, reqID string, status int) chat.Conversation {
		return request(t, client, "POST", url+"/messages"+scope, "alice", fmt.Sprintf(`{"request_id":%q,"content":%q,"mode":%q}`, reqID, content, mode), status)
	}
	first := submit("First task", "queue", uuid.NewString(), 202)
	submit("Invalid mode", "execute", uuid.NewString(), 400)
	// Reopen storage before workers exist: accepted queue is not a browser artifact.
	reopened := liveStore(t)
	pending, err := reopened.Get(context.Background(), id)
	if err != nil || !pending.Active() {
		t.Fatal("queued job not durable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		go func() { app.Run(ctx); done <- struct{}{} }()
	}
	defer func() { cancel(); <-done; <-done }()
	wait := func(check func(chat.Conversation) bool) chat.Conversation {
		t.Helper()
		end := time.Now().Add(10 * time.Second)
		for time.Now().Before(end) {
			c := request(t, client, "GET", url+scope, "alice", "", 200)
			if check(c) {
				return c
			}
			time.Sleep(40 * time.Millisecond)
		}
		t.Fatal("queue did not reach expected state")
		return chat.Conversation{}
	}
	wait(func(c chat.Conversation) bool { return c.Messages[1].Content != "" })
	a := submit("Queued alpha", "queue", uuid.NewString(), 202)
	b := submit("Queued beta", "queue", uuid.NewString(), 202)
	before := len(b.Messages[1].Content)
	wait(func(c chat.Conversation) bool {
		return c.Jobs[0].Status == "running" && len(c.Messages[1].Content) > before
	})
	priorityID := uuid.NewString()
	interrupted := submit("Urgent priority", "stop_and_send", priorityID, 202)
	if interrupted.Jobs[0].Status != "cancelled" || interrupted.Jobs[1].RequestID != priorityID || interrupted.Jobs[2].ID != a.Jobs[1].ID {
		t.Fatal("interrupt did not preserve queue with priority")
	}
	submit("Urgent priority", "stop_and_send", priorityID, 200)
	submit("Urgent priority", "queue", priorityID, 409)
	request(t, client, "POST", url+"/messages"+scope, "bob", fmt.Sprintf(`{"request_id":%q,"content":"forbidden","mode":"queue"}`, uuid.NewString()), 404)
	final := wait(func(c chat.Conversation) bool { return !c.Active() })
	if len(final.Jobs) != 4 || final.Jobs[0].ID != first.Jobs[0].ID || final.Jobs[0].Status != "cancelled" || final.Messages[1].Content == "" {
		t.Fatal("cancelled partial response lost or duplicate job created")
	}
	for _, j := range final.Jobs[1:] {
		if j.Status != "completed" {
			t.Fatalf("queued job ended as %s", j.Status)
		}
	}
	mu.Lock()
	captured := append([][]chat.Message(nil), prompts...)
	mu.Unlock()
	if len(captured) != 4 {
		t.Fatalf("expected four provider calls, got %d", len(captured))
	}
	for i, want := range []string{"First task", "Urgent priority", "Queued alpha", "Queued beta"} {
		messages := captured[i]
		if messages[len(messages)-1].Content != want {
			t.Fatalf("wrong queue order for call %d", i)
		}
	}
	for _, message := range captured[1] {
		if message.Content == "Queued alpha" || message.Content == "Queued beta" {
			t.Fatal("future queued input leaked into priority prompt")
		}
	}
	// Retrying interruption after completion cannot cancel another response.
	retried := submit("Urgent priority", "stop_and_send", priorityID, 200)
	if retried.Revision != final.Revision {
		t.Fatal("idempotent interrupt mutated conversation")
	}
}
