package workflows_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/example/runloom/workflows/internal/authoring"
	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/engine"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/google/uuid"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

func buildingSample(t *testing.T) model.Definition {
	t.Helper()
	raw, err := os.ReadFile("../testdata/examples/data-building.json")
	require.NoError(t, err)
	var value any
	require.NoError(t, json.Unmarshal(raw, &value))
	compiler := jsonschema.NewCompiler()
	schema, err := compiler.Compile("../testdata/spec/workflow.schema.json")
	require.NoError(t, err)
	require.NoError(t, schema.Validate(value))
	var d model.Definition
	require.NoError(t, json.Unmarshal(raw, &d))
	d, err = definition.Normalize(d)
	require.NoError(t, err)
	return d
}

func TestBuildingSampleAndAuthoring(t *testing.T) {
	d := buildingSample(t)
	scope := authoring.Context{RootID: "parent"}
	root := model.Definition{SchemaVersion: "1.0", ID: "parent", Name: "Parent", Steps: []model.Step{}}
	updated, child, err := authoring.Build(root, scope, "child-data", authoring.Proposal{Name: "Prepare data", Steps: d.Steps})
	require.NoError(t, err)
	require.Equal(t, "child", child.Kind)
	require.Equal(t, "child-data", updated.Steps[0].DefinitionID)
}

// Opt in with an isolated local Temporal server. No real external systems are called.
func TestBuildingLiveReplay(t *testing.T) {
	address := os.Getenv("RUNLOOM_TEST_TEMPORAL_ADDRESS")
	if address == "" {
		t.Skip("set RUNLOOM_TEST_TEMPORAL_ADDRESS to a disposable local server")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c, err := client.Dial(client.Options{HostPort: address})
	require.NoError(t, err)
	defer c.Close()
	queue := "building-test-" + uuid.NewString()
	w := worker.New(c, queue, worker.Options{})
	w.RegisterWorkflow(engine.RunloomWorkflow)
	require.NoError(t, w.Start())
	defer w.Stop()
	d := buildingSample(t)
	// A durable timer per item ensures replay crosses workflow tasks, not just
	// one synchronous in-memory evaluation.
	d.Steps[1].Branches[0].Steps = append([]model.Step{{ID: "delay", Type: "wait", Seconds: 1}}, d.Steps[1].Branches[0].Steps...)
	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{ID: queue, TaskQueue: queue}, engine.RunloomWorkflow, model.RunInput{
		Definition: d,
		Input: map[string]any{"customers": []any{
			map[string]any{"name": "Ada", "active": true},
			map[string]any{"name": "Grace", "email": "grace@example.test", "active": true},
			map[string]any{"name": "Inactive", "active": false},
		}},
	})
	require.NoError(t, err)
	var result model.RunResult
	require.NoError(t, run.Get(ctx, &result))
	require.Equal(t, "completed", result.Status)
	require.Equal(t, map[string]any{"count": float64(2), "customers": []any{
		map[string]any{"name": "Ada", "email": ""},
		map[string]any{"name": "Grace", "email": "grace@example.test"},
	}}, result.Output)
	history := &historypb.History{}
	iter := c.GetWorkflowHistory(ctx, run.GetID(), run.GetRunID(), false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	for iter.HasNext() {
		event, err := iter.Next()
		require.NoError(t, err)
		history.Events = append(history.Events, event)
	}
	replayer := worker.NewWorkflowReplayer()
	replayer.RegisterWorkflow(engine.RunloomWorkflow)
	require.NoError(t, replayer.ReplayWorkflowHistory(nil, history))
}
