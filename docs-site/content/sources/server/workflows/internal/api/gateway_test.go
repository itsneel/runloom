package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/runloom/workflows/internal/access"
	"github.com/example/runloom/workflows/internal/store"
)

type testVerifier map[string]access.User

func (v testVerifier) Verify(_ context.Context, token string) (access.User, error) {
	if u, ok := v[token]; ok {
		return u, nil
	}
	return access.User{}, errors.New("invalid")
}
func TestGatewayTenantAndRoleBoundaries(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	repo, err := access.Open(ctx, "file", "", "", root)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close(ctx)
	d := &access.Directory{Repo: repo}
	users := testVerifier{"alice": {ID: "alice", Email: "alice@example.com"}, "bob": {ID: "bob", Email: "bob@example.com"}, "viewer": {ID: "viewer", Email: "viewer@example.com"}}
	for _, u := range users {
		if _, err = d.Bind(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	orgA, err := d.CreateOrg(ctx, users["alice"], "A")
	if err != nil {
		t.Fatal(err)
	}
	orgB, err := d.CreateOrg(ctx, users["bob"], "B")
	if err != nil {
		t.Fatal(err)
	}
	s, _, _ := repo.Read(ctx)
	teamA, teamB := "", ""
	for id := range s.Orgs[orgA].Teams {
		teamA = id
	}
	for id := range s.Orgs[orgB].Teams {
		teamB = id
	}
	if err = d.SetMember(ctx, users["alice"], orgA, "", users["viewer"].Email, "member", false); err != nil {
		t.Fatal(err)
	}
	if err = d.SetMember(ctx, users["alice"], orgA, teamA, users["viewer"].Email, "viewer", false); err != nil {
		t.Fatal(err)
	}
	baseStore, _ := store.New(filepath.Join(root, "base"))
	base := New(baseStore, nil, "queue")
	g := NewGateway(base, d, users, access.Config{}, func(ctx context.Context, id string) (store.Backend, error) {
		return store.OpenTeam(ctx, "file", "", "", root, id)
	}, nil, "queue")
	defer g.Close()
	call := func(method, path, token, team, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		r.Header.Set("X-Runloom-Team", team)
		r.Header.Set("X-Runloom-Actor", "spoofed-actor")
		w := httptest.NewRecorder()
		g.ServeHTTP(w, r)
		return w
	}
	definition := `{"schema_version":"1.0","id":"shared-name","name":"Example","trigger":{"type":"manual"},"steps":[{"id":"echo","type":"activity","activity":"runloom.echo"}],"output":{}}`
	for _, test := range []struct {
		method, path, token, team, body string
		status                          int
	}{
		{"GET", "/api/v1/studio/definitions", "", teamA, "", 401},
		{"GET", "/api/v1/studio/definitions", "invalid", teamA, "", 401},
		{"GET", "/api/v1/studio/runs/private/llm-usage", "", teamA, "", 401},
		{"GET", "/api/v1/studio/runs/private/llm-usage", "bob", teamA, "", 403},
		{"GET", "/api/v1/studio/definitions", "bob", teamA, "", 403},
		{"GET", "/api/v1/studio/definitions", "alice", "", "", 403},
		{"POST", "/api/v1/studio/definitions", "viewer", teamA, definition, 403},
		{"POST", "/api/v1/studio/definitions/shared-name/authoring", "viewer", teamA, `{}`, 403},
		{"POST", "/api/v1/studio/authoring/workflows", "viewer", teamA, `{}`, 403},
		{"POST", "/api/v1/studio/authoring/workflows", "bob", teamA, `{}`, 403},
		{"POST", "/api/v1/studio/authoring/workflows", "", teamA, `{}`, 401},
		{"GET", "/api/v1/studio/authoring/workflows", "bob", teamA, "", 403},
		{"POST", "/api/v1/studio/definitions/shared-name/authoring", "bob", teamA, `{}`, 403},
		{"POST", "/api/v1/studio/definitions/shared-name/authoring", "", teamA, `{}`, 401},
		{"GET", "/api/v1/studio/definitions/shared-name/authoring", "bob", teamA, "", 403},
		{"GET", "/api/v1/studio/runs", "viewer", teamA, "", 403},
		{"GET", "/api/v1/studio/audit", "viewer", teamA, "", 403},
		{"GET", "/api/v1/studio/schedules", "viewer", teamA, "", 403},
		{"POST", "/api/v1/studio/code/test", "viewer", teamA, `{}`, 403},
		{"POST", "/api/v1/studio/code/test", "", teamA, `{}`, 401},
		{"POST", "/api/v1/studio/code/test", "bob", teamA, `{}`, 403},
		{"POST", "/api/v1/studio/code/test", "alice", teamA, `{}`, 422},
		{"POST", "/api/v1/studio/definitions/shared-name/edit-lock", "viewer", teamA, "", 403},
		{"PUT", "/api/v1/studio/definitions/shared-name/edit-lock", "viewer", teamA, "", 403},
		{"DELETE", "/api/v1/studio/definitions/shared-name/edit-lock", "viewer", teamA, "", 403},
		{"GET", "/api/v1/studio/definitions/shared-name/edit-lock", "bob", teamA, "", 403},
		{"GET", "/api/v1/studio/definitions/shared-name/edit-lock", "viewer", teamA, "", 200},
		{"POST", "/api/v1/studio/schedules", "viewer", teamA, `{}`, 403},
		{"POST", "/api/v1/studio/definitions/child/test", "viewer", teamA, `{}`, 403},
		{"POST", "/api/v1/studio/definitions/child/test", "", teamA, `{}`, 401},
		{"POST", "/api/v1/studio/definitions/child/test", "bob", teamA, `{}`, 403},
		{"POST", "/api/v1/studio/definitions/child/test", "bob", teamB, `{"version":1}`, 404},
		{"POST", "/api/v1/studio/schedules/test/pause", "viewer", teamA, `{}`, 403},
		{"POST", "/api/v1/studio/schedules/test/resume", "viewer", teamA, `{}`, 403},
		{"POST", "/api/v1/studio/schedules/test/trigger", "viewer", teamA, `{}`, 403},
		{"DELETE", "/api/v1/studio/schedules/test", "viewer", teamA, "", 403},
		{"POST", "/api/v1/studio/definitions", "alice", teamA, definition, 200},
		{"GET", "/api/v1/studio/definitions/shared-name?version=0", "bob", teamB, "", 404},
		{"POST", "/api/v1/studio/definitions", "bob", teamB, definition, 200},
		{"GET", "/api/v1/studio/definitions/shared-name?version=0", "viewer", teamA, "", 200},
		{"POST", "/api/v1/studio/orgs/" + orgA + "/teams", "viewer", "", `{"name":"Unauthorized"}`, 403},
		{"POST", "/api/v1/studio/orgs/" + orgA + "/members", "bob", "", `{"email":"bob@example.com","role":"owner"}`, 403},
	} {
		got := call(test.method, test.path, test.token, test.team, test.body)
		if got.Code != test.status {
			t.Fatalf("%s %s as %s: got %d want %d (%s)", test.method, test.path, test.token, got.Code, test.status, got.Body.String())
		}
	}
	a, _ := g.tenant(ctx, teamA)
	projectResponse := call("POST", "/api/v1/studio/projects", "alice", teamA, `{"name":"Private project"}`)
	if projectResponse.Code != 201 {
		t.Fatal(projectResponse.Body.String())
	}
	var project store.Project
	if err := json.Unmarshal(projectResponse.Body.Bytes(), &project); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		method, path, token, team, body string
		status                          int
	}{
		{"GET", "/api/v1/studio/projects", "", teamA, "", 401},
		{"GET", "/api/v1/studio/projects", "bob", teamA, "", 403},
		{"GET", "/api/v1/studio/projects", "viewer", teamA, "", 200},
		{"POST", "/api/v1/studio/projects", "viewer", teamA, `{"name":"Denied"}`, 403},
		{"POST", "/api/v1/studio/projects/" + project.ID + "/rename", "viewer", teamA, `{"name":"Denied"}`, 403},
		{"POST", "/api/v1/studio/projects/" + project.ID + "/rename", "bob", teamB, `{"name":"Denied"}`, 404},
		{"POST", "/api/v1/studio/definitions/shared-name/project", "viewer", teamA, `{"project_id":"default"}`, 403},
		{"POST", "/api/v1/studio/definitions/shared-name/project", "bob", teamB, `{"project_id":"` + project.ID + `"}`, 404},
		{"POST", "/api/v1/studio/definitions/shared-name/project", "alice", teamA, `{"project_id":"` + project.ID + `"}`, 200},
	} {
		w := call(test.method, test.path, test.token, test.team, test.body)
		if w.Code != test.status {
			t.Fatalf("project authorization %s %s: %d, %s", test.method, test.path, w.Code, w.Body.String())
		}
	}
	if w := call("GET", "/api/v1/studio/projects", "bob", teamB, ""); strings.Contains(w.Body.String(), project.ID) {
		t.Fatal("project leaked across teams")
	}
	commits, err := a.backend.ListCommits("shared-name", 10)
	projectAudits, auditErr := a.backend.ListAudit("", "", 100)
	if auditErr != nil {
		t.Fatal(auditErr)
	}
	for _, event := range projectAudits {
		if (event.EventType == "project.created" || event.EventType == "definition.project_moved") && event.Actor != "alice" {
			t.Fatal("project audit actor spoofing succeeded")
		}
	}
	if err != nil || commits[0].Actor != "alice" {
		t.Fatal("actor spoofing succeeded", err)
	}
	b, _ := g.tenant(ctx, teamB)
	_, err = b.backend.RecordRunStart(store.RunRecord{WorkflowID: "other-team-run", TemporalStatus: "completed", Input: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ method, path string }{{"GET", "/api/v1/studio/runs/other-team-run"}, {"GET", "/api/v1/studio/runs/other-team-run/llm-usage"}, {"GET", "/api/v1/studio/runs/other-team-run/history"}, {"POST", "/api/v1/studio/runs/other-team-run/cancel"}, {"POST", "/api/v1/studio/callbacks/other-team-run/echo"}, {"POST", "/api/v1/studio/runs/other-team-run/signals/ready"}} {
		if w := call(test.method, test.path, "alice", teamA, `{}`); w.Code != 404 {
			t.Fatal("cross-team Temporal operation accepted", test.path, w.Code)
		}
	}
	w := call("GET", "/api/v1/studio/me", "alice", "", "")
	var profile access.Overview
	if err = json.Unmarshal(w.Body.Bytes(), &profile); err != nil || len(profile.Organizations) != 1 || profile.Organizations[0].ID != orgA {
		t.Fatal("organization list leaked", err)
	}
	if err = d.SetMember(ctx, users["alice"], orgA, teamA, users["viewer"].Email, "", true); err != nil {
		t.Fatal(err)
	}
	if w = call("GET", "/api/v1/studio/definitions", "viewer", teamA, ""); w.Code != 403 {
		t.Fatal("role revocation did not take effect")
	}
}
