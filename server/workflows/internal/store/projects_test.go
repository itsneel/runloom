package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func exerciseProjects(t *testing.T, s Backend) {
	t.Helper()
	ctx := context.Background()
	name := "Finance-" + uuid.NewString()
	p, err := s.CreateProject(ctx, "  "+name+"  ")
	require.NoError(t, err)
	require.Equal(t, name, p.Name)
	_, err = s.CreateProject(ctx, strings.ToUpper(name))
	require.ErrorIs(t, err, ErrProjectConflict)
	for _, invalid := range []string{"", "  ", strings.Repeat("x", 81), "Bad\nName"} {
		_, err = s.CreateProject(ctx, invalid)
		require.ErrorIs(t, err, ErrProjectName)
	}
	_, err = s.RenameProject(ctx, "missing", "Missing")
	require.ErrorIs(t, err, ErrProjectNotFound)
	_, err = s.RenameProject(ctx, p.ID, "General")
	require.ErrorIs(t, err, ErrProjectConflict)
	p, err = s.RenameProject(ctx, p.ID, name+" renamed")
	require.NoError(t, err)
	require.Equal(t, name+" renamed", p.Name)

	value := exampleDefinition(t)
	value.ID = "project-test-" + uuid.NewString()
	_, err = s.SaveDraft(value)
	require.NoError(t, err)
	before, err := s.Publish(value.ID)
	require.NoError(t, err)
	commitsBefore, err := s.ListCommits(value.ID, 20)
	require.NoError(t, err)
	catalog, err := s.ProjectCatalog(ctx)
	require.NoError(t, err)
	require.Equal(t, DefaultProjectID, catalog.WorkflowProject(value.ID))
	require.NoError(t, s.AssignProject(ctx, value.ID, p.ID))
	catalog, err = s.ProjectCatalog(ctx)
	require.NoError(t, err)
	require.Equal(t, p.ID, catalog.WorkflowProject(value.ID))
	require.ErrorIs(t, s.AssignProject(ctx, value.ID, "missing"), ErrProjectNotFound)
	require.ErrorIs(t, s.AssignProject(ctx, "missing-workflow", p.ID), ErrNotFound)
	require.ErrorIs(t, s.AssignProject(ctx, "../outside", p.ID), ErrNotFound)
	after, err := s.Get(value.ID, nil)
	require.NoError(t, err)
	require.Equal(t, before, after)
	commitsAfter, err := s.ListCommits(value.ID, 20)
	require.NoError(t, err)
	require.Equal(t, commitsBefore, commitsAfter, "moving does not create workflow commits")
	_, _, err = s.RestoreCommit(value.ID, commitsBefore[len(commitsBefore)-1].CommitID, CommitMeta{Actor: "test"})
	require.NoError(t, err)
	catalog, err = s.ProjectCatalog(ctx)
	require.NoError(t, err)
	require.Equal(t, p.ID, catalog.WorkflowProject(value.ID), "restore does not move the workflow")
	require.NoError(t, s.AssignProject(ctx, value.ID, DefaultProjectID))

	// Concurrent requests with the same normalized name must create one folder.
	var wg sync.WaitGroup
	results := make(chan error, 8)
	duplicateName := "Concurrent-" + uuid.NewString()
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.CreateProject(ctx, duplicateName); results <- err }()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			require.True(t, errors.Is(err, ErrProjectConflict), err)
		}
	}
	require.Equal(t, 1, successes)
}

func TestFileProjects(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	require.NoError(t, err)
	exerciseProjects(t, s)
	before, err := s.ProjectCatalog(context.Background())
	require.NoError(t, err)
	reopened, err := New(root)
	require.NoError(t, err)
	after, err := reopened.ProjectCatalog(context.Background())
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestProjectCatalogReopenAndReplicaUpdates(t *testing.T) {
	for _, test := range []struct{ driver, variable string }{
		{"file", ""}, {"postgres", "RUNLOOM_TEST_POSTGRES_URL"}, {"mongodb", "RUNLOOM_TEST_MONGODB_URL"},
	} {
		t.Run(test.driver, func(t *testing.T) {
			url := os.Getenv(test.variable)
			if test.variable != "" && url == "" {
				t.Skip("disposable database URL not configured")
			}
			ctx := context.Background()
			root, team := t.TempDir(), uuid.NewString()
			open := func() Backend {
				s, err := OpenTeam(ctx, test.driver, url, "runloom_project_reopen_test", root, team)
				require.NoError(t, err)
				return s
			}
			first := open()
			p, err := first.CreateProject(ctx, "Persisted project")
			require.NoError(t, err)
			value := exampleDefinition(t)
			_, err = first.SaveDraft(value)
			require.NoError(t, err)
			require.NoError(t, first.AssignProject(ctx, value.ID, p.ID))
			require.NoError(t, first.Close(ctx))
			second := open()
			defer second.Close(ctx)
			catalog, err := second.ProjectCatalog(ctx)
			require.NoError(t, err)
			require.Equal(t, p, catalog.Projects[p.ID])
			require.Equal(t, p.ID, catalog.WorkflowProject(value.ID))
			if test.driver == "file" {
				return
			}
			third := open()
			defer third.Close(ctx)
			var wg sync.WaitGroup
			results := make(chan error, 2)
			for _, replica := range []Backend{second, third} {
				wg.Add(1)
				go func(s Backend) {
					defer wg.Done()
					_, err := s.CreateProject(ctx, "Same name across replicas")
					results <- err
				}(replica)
			}
			wg.Wait()
			close(results)
			created := 0
			for err := range results {
				if err == nil {
					created++
				} else {
					require.ErrorIs(t, err, ErrProjectConflict)
				}
			}
			require.Equal(t, 1, created)
		})
	}
}
