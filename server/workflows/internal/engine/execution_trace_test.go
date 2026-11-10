package engine

import (
	"strings"
	"testing"

	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

func TestExecutionTraceShowsParallelTimingsResolvedPayloadsAndFailure(t *testing.T) {
	suite := testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	d := model.Definition{Steps: []model.Step{
		{ID: "fork", Type: "parallel", Branches: []model.Branch{
			{ID: "short", Steps: []model.Step{{ID: "one", Type: "wait", Seconds: 1}}},
			{ID: "long", Steps: []model.Step{{ID: "three", Type: "wait", Seconds: 3}}},
		}},
		{ID: "stop", Type: "fail", Message: "${input.reason}"},
		{ID: "never", Type: "end"},
	}}
	env.ExecuteWorkflow(RunloomWorkflow, model.RunInput{Definition: d, Input: map[string]any{"reason": "invalid order"}})
	require.Error(t, env.GetWorkflowError())
	q, err := env.QueryWorkflow("execution_trace")
	require.NoError(t, err)
	var trace model.ExecutionTrace
	require.NoError(t, q.Get(&trace))
	require.Len(t, trace.Nodes, 4)
	fork, first, second, failure := trace.Nodes[0], trace.Nodes[1], trace.Nodes[2], trace.Nodes[3]
	require.Equal(t, fork.Sequence, first.Parent)
	require.Equal(t, fork.Sequence, second.Parent)
	require.Equal(t, first.StartedAt, second.StartedAt)
	require.NotEqual(t, first.FinishedAt, second.FinishedAt)
	require.JSONEq(t, `{"seconds":1}`, string(first.Input))
	require.JSONEq(t, `{"waited_seconds":1}`, string(first.Output))
	require.Equal(t, "failed", failure.Status)
	require.JSONEq(t, `"invalid order"`, string(failure.Input))
	require.Contains(t, failure.Error, "invalid order")
	require.Nil(t, failure.Output)
}

func TestExecutionTraceBoundsPreviewsAndKeepsNull(t *testing.T) {
	trace := &executionTrace{}
	raw, omitted := trace.preview(nil)
	require.False(t, omitted)
	require.Equal(t, "null", string(raw))
	raw, omitted = trace.preview(strings.Repeat("x", 8192))
	require.True(t, omitted)
	require.Empty(t, raw)
}
