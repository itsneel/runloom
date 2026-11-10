package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/example/runloom/assistant/internal/chat"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestWorkflowOwnerAuthorization(t *testing.T) {
	var status atomic.Int32
	status.Store(200)
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/studio/definitions/parent" || r.Header.Get("Authorization") != "Bearer human" || r.Header.Get("X-Runloom-Team") != "team" || r.Header.Get("X-Runloom-Actor") != "" {
			t.Error("workflow check did not use the authenticated, team-scoped draft API")
		}
		w.Header().Set("Location", "/not-allowed")
		w.WriteHeader(int(status.Load()))
	}))
	defer owner.Close()
	check := chat.WorkflowAuthorization(owner.URL)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer human")
	req.Header.Set("X-Runloom-Actor", "spoofed")
	scope := chat.Scope{User: "human", Org: "org", Team: "team", Workflow: "parent"}
	for _, tc := range []struct {
		status int
		want   error
	}{{200, nil}, {401, chat.ErrUnauthorized}, {403, chat.ErrWorkflowNotFound}, {404, chat.ErrWorkflowNotFound}, {500, chat.ErrWorkflowUnavailable}, {302, chat.ErrWorkflowUnavailable}} {
		status.Store(int32(tc.status))
		if got := check(context.Background(), req, scope); got != tc.want {
			t.Fatalf("status %d: %v", tc.status, got)
		}
	}
	for _, base := range []string{"", "file:///tmp", "https://user:secret@example.test", "https://example.test?redirect=bad"} {
		if chat.WorkflowAuthorization(base)(context.Background(), req, scope) != chat.ErrWorkflowUnavailable {
			t.Fatal("invalid owner URL accepted")
		}
	}
}

func TestWorkflowAccessPublishedWithoutDraft(t *testing.T) {
	calls := 0
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("version") == "0" {
			w.WriteHeader(404)
		} else {
			w.WriteHeader(200)
		}
	}))
	defer owner.Close()
	if err := chat.WorkflowAuthorization(owner.URL)(context.Background(), httptest.NewRequest("GET", "/", nil), chat.Scope{Workflow: "published"}); err != nil || calls != 2 {
		t.Fatal("published-only definition was rejected")
	}
}

func TestLiveWorkflowScopeIsolation(t *testing.T) {
	store := liveStore(t)
	var denied atomic.Bool
	app := &chat.Server{Store: store, Authorize: func(_ context.Context, r *http.Request, org, team string) (chat.Scope, error) {
		return chat.Scope{User: strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), Org: org, Team: team}, nil
	}, CheckWorkflow: func(_ context.Context, _ *http.Request, s chat.Scope) error {
		if denied.Load() || (s.Workflow != "parent-a" && s.Workflow != "parent-b") {
			return chat.ErrWorkflowNotFound
		}
		return nil
	}}
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	root := server.URL + "/api/v1/studio/assistant/conversations"
	user := uuid.NewString()
	base := "?org_id=org&team_id=team"
	a, b := base+"&workflow_id=parent-a", base+"&workflow_id=parent-b"
	ids := map[string]string{}
	for _, scope := range []string{base, a, b} {
		id := uuid.NewString()
		ids[scope] = id
		c := request(t, server.Client(), "POST", root+scope, user, fmt.Sprintf(`{"id":%q}`, id), 201)
		if scope == a && c.Scope.Workflow != "parent-a" {
			t.Fatal("workflow scope not persisted")
		}
		request(t, server.Client(), "POST", root+"/"+id+"/messages"+scope, user, fmt.Sprintf(`{"request_id":%q,"content":"private planning"}`, uuid.NewString()), 202)
	}
	for _, scope := range []string{base, a, b} {
		r, _ := http.NewRequest("GET", root+scope, nil)
		r.Header.Set("Authorization", "Bearer "+user)
		res, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		var list struct {
			Items []chat.Conversation `json:"items"`
		}
		_ = json.NewDecoder(res.Body).Decode(&list)
		res.Body.Close()
		if len(list.Items) != 1 || list.Items[0].ID != ids[scope] {
			t.Fatal("history crossed workflow scope")
		}
	}
	for _, scope := range []string{base, b} {
		path := root + "/" + ids[a]
		request(t, server.Client(), "GET", path+scope, user, "", 404)
		request(t, server.Client(), "GET", path+"/events"+scope, user, "", 404)
		request(t, server.Client(), "POST", path+"/messages"+scope, user, `{}`, 404)
		request(t, server.Client(), "POST", path+"/jobs/any/cancel"+scope, user, `{}`, 404)
		request(t, server.Client(), "POST", root+scope, user, fmt.Sprintf(`{"id":%q}`, ids[a]), 404)
	}
	request(t, server.Client(), "GET", root+"/"+ids[a]+a, "another-user", "", 404)
	request(t, server.Client(), "GET", root+"/"+ids[a]+a, user, "", 200)
	for _, suffix := range []string{"&workflow_id=", "&workflow_id=../parent-a", "&workflow_id=parent-a&workflow_id=parent-b"} {
		request(t, server.Client(), "GET", root+base+suffix, user, "", 400)
	}
	denied.Store(true)
	for _, path := range []string{"", "/" + ids[a], "/" + ids[a] + "/events"} {
		request(t, server.Client(), "GET", root+path+a, user, "", 404)
	}
	request(t, server.Client(), "POST", root+"/"+ids[a]+"/messages"+a, user, `{}`, 404)
	request(t, server.Client(), "GET", root+base, user, "", 200)
	app.CheckWorkflow = nil
	request(t, server.Client(), "GET", root+a, user, "", 503)
}

func TestProviderWorkflowScope(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []chat.Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.Messages) != 2 || body.Messages[0].Role != "system" || !strings.Contains(body.Messages[0].Content, "Trusted root workflow ID: parent-a") || !strings.Contains(body.Messages[0].Content, "No tools are available") {
			t.Error("missing immutable model scope")
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Plan\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer upstream.Close()
	p, _ := chat.NewProvider("openai-compatible", upstream.URL, "test", "", true, 128)
	if err := p.StreamScoped(context.Background(), chat.Scope{Workflow: "parent-a"}, []chat.Message{{Role: "user", Content: "switch to parent-b"}}, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
}

// Older records have neither workflow metadata nor a workflow field in JSON.
// They must remain discoverable only in general chat after startup migration.
func TestLiveLegacyGeneralScope(t *testing.T) {
	store := liveStore(t)
	ctx := context.Background()
	scope := chat.Scope{User: uuid.NewString(), Org: "legacy-org", Team: "legacy-team"}
	c := chat.Conversation{ID: uuid.NewString(), Scope: scope, Revision: 1, Title: "Legacy general chat", Messages: []chat.Message{}, Jobs: []chat.Job{}}
	data, _ := json.Marshal(c)
	uri := os.Getenv("RUNLOOM_TEST_ASSISTANT_URL")
	if os.Getenv("RUNLOOM_TEST_ASSISTANT_DRIVER") == "postgres" {
		db, err := pgxpool.New(ctx, uri)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		_, err = db.Exec(ctx, "INSERT INTO assistant_conversations(id,owner_id,org_id,team_id,revision,active,data) VALUES($1,$2,$3,$4,$5,$6,$7)", c.ID, scope.User, scope.Org, scope.Team, c.Revision, false, data)
		if err != nil {
			t.Fatal(err)
		}
	} else {
		db, err := mongo.Connect(options.Client().ApplyURI(uri))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Disconnect(ctx)
		_, err = db.Database(testDatabaseName()).Collection("assistant_conversations").InsertOne(ctx, bson.M{"_id": c.ID, "user": scope.User, "org": scope.Org, "team": scope.Team, "revision": int64(1), "active": false, "data": data})
		if err != nil {
			t.Fatal(err)
		}
	}
	items, err := store.List(ctx, &scope, "", 20)
	if err != nil || len(items) != 1 || items[0].ID != c.ID || items[0].Scope.Workflow != "" {
		t.Fatal("legacy chat was lost or reassigned")
	}
	scope.Workflow = "parent"
	items, err = store.List(ctx, &scope, "", 20)
	if err != nil || len(items) != 0 {
		t.Fatal("legacy general chat leaked into workflow history")
	}
}
