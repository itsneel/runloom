package workflows_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/example/runloom/workflows/internal/access"
	"github.com/example/runloom/workflows/internal/api"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func deletionDefinition(id string) model.Definition {
	return model.Definition{ID: id, Name: "Draft workflow", SchemaVersion: "1.0", Trigger: model.Trigger{Type: "manual"}, Steps: []model.Step{{ID: "echo", Type: "activity", Activity: "runloom.echo"}}}
}

func TestDeletionStoreContract(t *testing.T) {
	for _, driver := range []string{"file", "postgres", "mongodb"} {
		t.Run(driver, func(t *testing.T) {
			url := ""
			if driver != "file" {
				key := "RUNLOOM_TEST_POSTGRES_URL"
				if driver == "mongodb" {
					key = "RUNLOOM_TEST_MONGODB_URL"
				}
				url = os.Getenv(key)
				if url == "" {
					t.Skip("disposable database not configured")
				}
			}
			ctx := context.Background()
			b, err := store.OpenTeam(ctx, driver, url, "runloom_deletion_test", t.TempDir(), uuid.NewString())
			require.NoError(t, err)
			defer b.Close(ctx)
			p, err := b.CreateProject(ctx, "Empty")
			require.NoError(t, err)
			require.NoError(t, b.DeleteProject(ctx, p.ID))
			require.ErrorIs(t, b.DeleteProject(ctx, p.ID), store.ErrProjectNotFound)
			require.ErrorIs(t, b.DeleteProject(ctx, "default"), store.ErrDefaultProject)
			p, err = b.CreateProject(ctx, "Contains draft")
			require.NoError(t, err)
			_, commit, err := b.CommitDraft(deletionDefinition("draft"), store.CommitMeta{Actor: "tester"})
			require.NoError(t, err)
			require.NoError(t, b.AssignProject(ctx, "draft", p.ID))
			require.ErrorIs(t, b.DeleteProject(ctx, p.ID), store.ErrProjectNotEmpty)
			_, err = b.RecordRunStart(store.RunRecord{WorkflowID: "test-run", TemporalRunID: "synthetic", DefinitionID: "draft", DefinitionVersion: 0, TriggerType: "manual", Input: map[string]any{"fixture": true}, TemporalStatus: "completed"})
			require.NoError(t, err)
			_, err = b.AppendAudit(store.AuditEvent{DefinitionID: "draft", EventType: "test.retained", Actor: "tester", Payload: map[string]any{}})
			require.NoError(t, err)
			require.NoError(t, b.DeleteDraft(ctx, "draft"))
			v := 0
			_, err = b.Get("draft", &v)
			require.ErrorIs(t, err, store.ErrNotFound)
			list, err := b.List()
			require.NoError(t, err)
			require.Empty(t, list)
			_, err = b.GetCommit("draft", commit.CommitID)
			require.NoError(t, err, "commit history retained")
			_, err = b.GetRun("test-run")
			require.NoError(t, err, "test runs retained")
			audits, err := b.ListAudit("", "draft", 10)
			require.NoError(t, err)
			require.NotEmpty(t, audits)
			require.NoError(t, b.DeleteProject(ctx, p.ID), "membership removed together with draft")
			require.ErrorIs(t, b.DeleteDraft(ctx, "draft"), store.ErrNotFound)
			require.ErrorIs(t, b.DeleteDraft(ctx, "../draft"), store.ErrNotFound)
			_, err = b.SaveDraft(deletionDefinition("published"))
			require.NoError(t, err)
			_, err = b.Publish("published")
			require.NoError(t, err)
			_, err = b.SaveDraft(deletionDefinition("published"))
			require.NoError(t, err)
			require.ErrorIs(t, b.DeleteDraft(ctx, "published"), store.ErrPublishedWorkflow)
			_, err = b.Get("published", nil)
			require.NoError(t, err)
			for i := 0; i < 5; i++ {
				id := "race-" + uuid.NewString()
				_, err = b.SaveDraft(deletionDefinition(id))
				require.NoError(t, err)
				var publishErr, deleteErr error
				var wg sync.WaitGroup
				wg.Add(2)
				start := make(chan struct{})
				go func() { defer wg.Done(); <-start; _, publishErr = b.Publish(id) }()
				go func() { defer wg.Done(); <-start; deleteErr = b.DeleteDraft(ctx, id) }()
				close(start)
				wg.Wait()
				require.False(t, publishErr == nil && deleteErr == nil, "publish and deletion cannot both succeed")
				if publishErr == nil {
					require.ErrorIs(t, deleteErr, store.ErrPublishedWorkflow)
				}
				if deleteErr == nil {
					require.Error(t, publishErr)
				}
			}
		})
	}
}

func TestDeletionAPI(t *testing.T) {
	ctx := context.Background()
	b, err := store.New(t.TempDir())
	require.NoError(t, err)
	s := api.New(b, nil, "test")
	defer s.Close()
	call := func(path string, status int) {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest("DELETE", "/api/v1/studio/"+path, nil)
		r.Header.Set("X-Runloom-Actor", "tester")
		s.Handler().ServeHTTP(w, r)
		require.Equal(t, status, w.Code, w.Body.String())
	}
	p, err := b.CreateProject(ctx, "Test")
	require.NoError(t, err)
	_, err = b.SaveDraft(deletionDefinition("draft"))
	require.NoError(t, err)
	require.NoError(t, b.AssignProject(ctx, "draft", p.ID))
	call("projects/"+p.ID, 409)
	token := uuid.NewString()
	_, err = b.ChangeEditLock(ctx, "draft", "someone", "Editor", token, "acquire")
	require.NoError(t, err)
	call("definitions/draft", 423)
	_, err = b.ChangeEditLock(ctx, "draft", "someone", "Editor", token, "release")
	require.NoError(t, err)
	call("definitions/draft", 204)
	call("definitions/draft", 404)
	call("projects/"+p.ID, 204)
	call("projects/default", 409)
	_, err = b.SaveDraft(deletionDefinition("published"))
	require.NoError(t, err)
	_, err = b.Publish("published")
	require.NoError(t, err)
	call("definitions/published", 409)
	events, err := b.ListAudit("", "", 100)
	require.NoError(t, err)
	raw, err := json.Marshal(events)
	require.NoError(t, err)
	require.Contains(t, string(raw), "definition.deleted")
	require.Contains(t, string(raw), "project.deleted")
}

type deletionVerifier map[string]access.User

func (v deletionVerifier) Verify(_ context.Context, token string) (access.User, error) {
	if user, ok := v[token]; ok {
		return user, nil
	}
	return access.User{}, errors.New("invalid test token")
}

func TestDeletionAuthorization(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	repo, err := access.Open(ctx, "file", "", "", root)
	require.NoError(t, err)
	defer repo.Close(ctx)
	d := &access.Directory{Repo: repo}
	users := deletionVerifier{"owner": {ID: "owner", Email: "owner@example.test"}, "viewer": {ID: "viewer", Email: "viewer@example.test"}, "other": {ID: "other", Email: "other@example.test"}}
	for _, u := range users {
		_, err = d.Bind(ctx, u)
		require.NoError(t, err)
	}
	org, err := d.CreateOrg(ctx, users["owner"], "Deletion test")
	require.NoError(t, err)
	state, _, err := repo.Read(ctx)
	require.NoError(t, err)
	team := ""
	for id := range state.Orgs[org].Teams {
		team = id
	}
	require.NoError(t, d.SetMember(ctx, users["owner"], org, "", users["viewer"].Email, "member", false))
	require.NoError(t, d.SetMember(ctx, users["owner"], org, team, users["viewer"].Email, "viewer", false))
	b, err := store.New(filepath.Join(root, "base"))
	require.NoError(t, err)
	g := api.NewGateway(api.New(b, nil, "test"), d, users, access.Config{}, func(ctx context.Context, id string) (store.Backend, error) {
		return store.OpenTeam(ctx, "file", "", "", root, id)
	}, nil, "test")
	defer g.Close()
	for _, path := range []string{"projects/fixture", "definitions/fixture"} {
		for _, test := range []struct {
			token  string
			status int
		}{{"", 401}, {"viewer", 403}, {"other", 403}} {
			r := httptest.NewRequest("DELETE", "/api/v1/studio/"+path, strings.NewReader(""))
			if test.token != "" {
				r.Header.Set("Authorization", "Bearer "+test.token)
			}
			r.Header.Set("X-Runloom-Team", team)
			w := httptest.NewRecorder()
			g.ServeHTTP(w, r)
			require.Equal(t, test.status, w.Code, w.Body.String())
		}
	}
}
