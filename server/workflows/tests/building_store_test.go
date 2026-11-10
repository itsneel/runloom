package workflows_test

import (
	"context"
	"os"
	"testing"

	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/llmcontract"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Run only against disposable databases through make test-authoring. Tests use
// a fresh team namespace; the runner deletes its containers afterwards.
func TestBuildingNodesStoreContract(t *testing.T) {
	for _, driver := range []string{"postgres", "mongodb"} {
		t.Run(driver, func(t *testing.T) {
			key := "RUNLOOM_TEST_POSTGRES_URL"
			if driver == "mongodb" {
				key = "RUNLOOM_TEST_MONGODB_URL"
			}
			url := os.Getenv(key)
			if url == "" {
				t.Skip("disposable database URL not configured")
			}
			b, err := store.OpenTeam(context.Background(), driver, url, "runloom_building_test", "", uuid.NewString())
			require.NoError(t, err)
			defer b.Close(context.Background())
			d := loopDefinition()
			d.Steps = append(d.Steps,
				model.Step{ID: "llm", Type: "llm", Prompt: "Summarize", Model: "fixture", LLMTask: &llmcontract.Task{Kind: "summarize"}},
				model.Step{ID: "notify", Type: "integration", Integration: &model.Integration{Operation: "slack-send", Secret: "fixture-token-name", Parameters: map[string]any{"channel": "fixture-channel", "text": "Ready"}}})
			d, err = definition.Normalize(d)
			require.NoError(t, err)
			_, err = b.SaveDraft(d)
			require.NoError(t, err)
			published, err := b.Publish(d.ID)
			require.NoError(t, err)
			version := published.Version
			roundTrip, err := b.Get(d.ID, &version)
			require.NoError(t, err)
			require.Equal(t, d.Steps, roundTrip.Steps)
			commits, err := b.ListCommits(d.ID, 10)
			require.NoError(t, err)
			require.Len(t, commits, 2)
			commit, err := b.GetCommit(d.ID, commits[1].CommitID)
			require.NoError(t, err)
			require.Equal(t, d.Steps, commit.Body.Steps)
		})
	}
}
