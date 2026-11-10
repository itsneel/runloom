package api

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/example/runloom/workflows/internal/budget"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/converter"
	"net/http/httptest"
	"testing"
)

type budgetExecutionClient struct{ *executionClient }

func (c budgetExecutionClient) QueryWorkflow(context.Context, string, string, string, ...any) (converter.EncodedValue, error) {
	p, _ := converter.GetDefaultDataConverter().ToPayload(model.RunState{Status: "paused", PausedStep: &model.PauseGate{StepID: "private-node", Message: "private-project", Budget: &budget.Decision{PolicyName: "sensitive-policy", ProjectID: "private-project", SpendUSD: "987654", Reason: "hard_limit_reached"}}})
	return executionValue{payload: p}, nil
}

func TestConsumerBudgetPauseIsRedacted(t *testing.T) {
	c := budgetExecutionClient{&executionClient{runs: map[string]enumspb.WorkflowExecutionStatus{}}}
	f := newConsumerFixtureClient(t, c, "queue")
	a := f.account()
	other := f.account()
	started := f.call("POST", "/api/v1/runs", a.ClientID, "budget-test", startBody())
	require.Equal(t, 202, started.Code)
	id := responseID(t, started)
	response := f.call("GET", "/api/v1/runs/"+id, a.ClientID, "", nil)
	require.Equal(t, 200, response.Code)
	require.Contains(t, response.Body.String(), `"status":"paused"`)
	require.Contains(t, response.Body.String(), `"pause_reason":"llm_budget_limit"`)
	for _, secret := range []string{"sensitive-policy", "private-project", "987654", "private-node"} {
		require.NotContains(t, response.Body.String(), secret)
	}
	require.Equal(t, 404, f.call("GET", "/api/v1/runs/"+id, other.ClientID, "", nil).Code)
}

func TestBudgetPoliciesAuthorizationAndRevision(t *testing.T) {
	f := newAnalyticsFixture(t)
	id := uuid.NewString()
	call := func(user, team, method string, p any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(p)
		url := "/api/v1/studio/orgs/" + f.org + "/teams/" + team + "/llm-budget-policies"
		if method == "PUT" {
			url += "/" + id
		}
		r := httptest.NewRequest(method, url, bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+user)
		w := httptest.NewRecorder()
		f.g.ServeHTTP(w, r)
		return w
	}
	p := budget.Policy{ID: id, Name: "Monthly", Enabled: true, SoftUSD: "100", HardUSD: "1000", Reason: "test"}
	for _, user := range []string{"viewer", "editor", "outsider"} {
		require.Equal(t, 403, call(user, f.team, "PUT", p).Code)
	}
	require.Equal(t, 401, call("machine", f.team, "PUT", p).Code)
	require.Equal(t, 403, call("admin", f.private, "PUT", p).Code)
	response := call("admin", f.team, "PUT", p)
	require.Equal(t, 200, response.Code, response.Body.String())
	require.Equal(t, 409, call("admin", f.team, "PUT", p).Code)
	for _, user := range []string{"owner", "orgadmin", "admin", "editor", "viewer"} {
		response = call(user, f.team, "GET", nil)
		require.Equal(t, 200, response.Code, response.Body.String())
		var result map[string]any
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		require.Equal(t, "UTC", result["timezone"])
		require.Len(t, result["items"], 1)
		require.Len(t, result["changes"], 1)
		require.Len(t, result["alerts"], 2)
	}
	require.Equal(t, 403, call("viewer", f.private, "GET", nil).Code)
	require.Equal(t, 401, call("machine", f.team, "GET", nil).Code)
	p.Revision = 1
	p.Projects = []string{"foreign-project"}
	require.Equal(t, 400, call("admin", f.team, "PUT", p).Code)
}
