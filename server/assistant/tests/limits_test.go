package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/runloom/assistant/internal/chat"
	"github.com/google/uuid"
)

func TestLiveLimitErrorsAndNewConversation(t *testing.T) {
	store := liveStore(t)
	scope := chat.Scope{User: "limit-test", Org: "org", Team: "team"}
	app := &chat.Server{Store: store, Authorize: func(context.Context, *http.Request, string, string) (chat.Scope, error) { return scope, nil }}
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	root := server.URL + "/api/v1/studio/assistant/conversations"
	query := "?org_id=org&team_id=team"
	for _, tc := range []struct {
		name, code, detail string
		jobs               int
		content            string
		queued             bool
	}{
		{"turns", "conversation_limit", "40 submissions", 40, "", false},
		{"bytes", "conversation_size_limit", "120,000 bytes", 0, strings.Repeat("a", 120000), false},
		{"queue", "queue_limit", "10 responses", 10, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.NewString()
			c := chat.Conversation{ID: id, Scope: scope, Revision: 1, Messages: []chat.Message{{ID: uuid.NewString(), Content: tc.content}}, Jobs: []chat.Job{}}
			for i := 0; i < tc.jobs; i++ {
				status := "completed"
				if tc.queued {
					status = "queued"
				}
				c.Jobs = append(c.Jobs, chat.Job{ID: uuid.NewString(), RequestID: uuid.NewString(), Status: status})
			}
			if err := store.Create(context.Background(), c); err != nil {
				t.Fatal(err)
			}
			body := fmt.Sprintf(`{"request_id":%q,"content":"Unsent test","mode":"queue"}`, uuid.NewString())
			res, err := server.Client().Post(root+"/"+id+"/messages"+query, "application/json", strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			var reply struct{ Code, Detail string }
			if err = json.NewDecoder(res.Body).Decode(&reply); err != nil {
				t.Fatal(err)
			}
			if res.StatusCode != 409 || reply.Code != tc.code || !strings.Contains(reply.Detail, tc.detail) {
				t.Fatalf("unexpected limit response: %d %+v", res.StatusCode, reply)
			}
			saved, err := store.Get(context.Background(), id)
			if err != nil || saved.Revision != 1 {
				t.Fatal("rejected message changed history")
			}
			next := uuid.NewString()
			request(t, server.Client(), "POST", root+query, "limit-test", fmt.Sprintf(`{"id":%q}`, next), 201)
			request(t, server.Client(), "POST", root+"/"+next+"/messages"+query, "limit-test", body, 202)
			retry := request(t, server.Client(), "POST", root+"/"+next+"/messages"+query, "limit-test", body, 200)
			if len(retry.Jobs) != 1 {
				t.Fatal("retry duplicated moved message")
			}
		})
	}
}
