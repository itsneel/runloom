package rexeralien

import (
	"context"
	"github.com/example/runloom/workflows/internal/engine"
	"github.com/example/runloom/workflows/internal/protection"
	"github.com/stretchr/testify/require"
	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"google.golang.org/protobuf/encoding/protojson"
	"os"
	"testing"
	"time"
)

// Opt-in only against the isolated installed local product. Replay registers no
// activities and therefore cannot call R2, the classifier or the QA receiver.
func TestInstalledLienHistoryReplay(t *testing.T) {
	id := os.Getenv("LIEN_REPLAY_WORKFLOW_ID")
	if id == "" {
		t.Skip("set LIEN_REPLAY_WORKFLOW_ID for installed local integration")
	}
	opts, err := protection.TemporalOptions("127.0.0.1:39233", os.Getenv("LIEN_REPLAY_NAMESPACE"))
	require.NoError(t, err)
	c, err := client.Dial(opts)
	require.NoError(t, err)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	iter := c.GetWorkflowHistory(ctx, id, os.Getenv("LIEN_REPLAY_RUN_ID"), false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	history := &historypb.History{}
	for iter.HasNext() {
		event, err := iter.Next()
		require.NoError(t, err)
		history.Events = append(history.Events, event)
	}
	require.NotEmpty(t, history.Events)
	replayer, err := worker.NewWorkflowReplayerWithOptions(worker.WorkflowReplayerOptions{DataConverter: opts.DataConverter, FailureConverter: opts.FailureConverter})
	require.NoError(t, err)
	replayer.RegisterWorkflow(engine.RunloomWorkflow)
	require.NoError(t, replayer.ReplayWorkflowHistory(nil, history))
	if path := os.Getenv("LIEN_REPLAY_HISTORY_PATH"); path != "" {
		raw, err := protojson.Marshal(history)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, raw, 0600))
	}
}
