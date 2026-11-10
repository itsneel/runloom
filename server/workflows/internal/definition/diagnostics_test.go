package definition

import (
	"encoding/json"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNodeIssuesCollectSiblingsAndChildrenWithoutMutating(t *testing.T) {
	d := model.Definition{Steps: []model.Step{
		{ID: "ask", Type: "llm", Model: "default"},
		{ID: "fork", Type: "parallel", Branches: []model.Branch{{ID: "a", Steps: []model.Step{{ID: "nested", Type: "http"}}}}},
		{ID: "stop", Type: "end"}, {ID: "delay", Type: "wait", Seconds: 1},
	}}
	before, _ := json.Marshal(d)
	issues := NodeIssues(d, nil)
	ids := []string{}
	for _, issue := range issues {
		ids = append(ids, issue.NodeID)
		require.NotEmpty(t, issue.Path)
		require.NotEmpty(t, issue.Message)
	}
	require.Equal(t, []string{"ask", "fork", "nested", "stop"}, ids)
	require.Equal(t, "steps[1].branches[0].steps[0]", issues[2].Path)
	after, _ := json.Marshal(d)
	require.Equal(t, string(before), string(after))
	d.Steps = []model.Step{{ID: "done", Type: "end"}}
	require.Empty(t, NodeIssues(d, nil))
}

func TestNodeIssuesUseStrictRulesForEveryNodeType(t *testing.T) {
	types := []string{"activity", "http", "email", "code", "llm", "decision", "subworkflow", "wait", "approval", "callback", "signal", "pause", "condition", "parallel", "unknown"}
	d := model.Definition{}
	for _, kind := range types {
		d.Steps = append(d.Steps, model.Step{ID: kind, Type: kind})
	}
	issues := NodeIssues(d, nil)
	require.Len(t, issues, len(types))
	for i, kind := range types {
		require.Equal(t, kind, issues[i].NodeID)
	}
}

func TestNodeIssuesDuplicateAndNestedDecision(t *testing.T) {
	d := model.Definition{Steps: []model.Step{{ID: "same", Type: "wait", Seconds: 1}, {ID: "same", Type: "wait", Seconds: 1}}}
	require.Contains(t, NodeIssues(d, nil)[0].Message, "duplicate step id")
	d.Steps = []model.Step{{ID: "outer", Type: "decision", Branches: []model.Branch{{ID: "action", Steps: []model.Step{{ID: "inner", Type: "decision"}}}}}}
	issues := NodeIssues(d, nil)
	require.Len(t, issues, 2)
	require.Equal(t, "inner", issues[1].NodeID)
	require.Contains(t, issues[1].Message, "nested AI Decision")
}
