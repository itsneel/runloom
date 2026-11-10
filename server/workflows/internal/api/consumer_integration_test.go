package api

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/example/runloom/workflows/internal/access"
	"github.com/example/runloom/workflows/internal/activities"
	"github.com/example/runloom/workflows/internal/engine"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

func TestConsumerExecutionLive(t *testing.T) {
	address := os.Getenv("RUNLOOM_TEST_TEMPORAL_ADDRESS")
	if address == "" {
		t.Skip("set RUNLOOM_TEST_TEMPORAL_ADDRESS for disposable Temporal checks")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := client.Dial(client.Options{HostPort: address})
	require.NoError(t, err)
	defer c.Close()
	queue := "consumer-" + uuid.NewString()
	f := newConsumerFixtureClient(t, c, queue)
	tenant, err := f.g.tenant(ctx, f.team)
	require.NoError(t, err)
	w := worker.New(c, queue, worker.Options{})
	w.RegisterWorkflow(engine.RunloomWorkflow)
	w.RegisterActivityWithOptions(activities.Echo, activity.RegisterOptions{Name: "runloom.echo"})
	require.NoError(t, w.Start())
	defer w.Stop()
	a := f.account()
	response := f.call("POST", "/api/v1/runs", a.ClientID, "real-temporal", startBody())
	require.Equal(t, 202, response.Code, response.Body.String())
	id := responseID(t, response)
	var result model.RunResult
	require.NoError(t, c.GetWorkflow(ctx, id, "").Get(ctx, &result))
	response = f.call("GET", "/api/v1/runs/"+id, a.ClientID, "", nil)
	require.Equal(t, 200, response.Code)
	require.Contains(t, response.Body.String(), `"status":"completed"`)
	first, err := c.DescribeWorkflowExecution(ctx, id, "")
	require.NoError(t, err)
	response = f.call("POST", "/api/v1/runs", a.ClientID, "real-temporal", startBody())
	require.Equal(t, 202, response.Code, response.Body.String())
	require.Equal(t, id, responseID(t, response))
	next, err := c.DescribeWorkflowExecution(ctx, id, "")
	require.NoError(t, err)
	require.Equal(t, first.WorkflowExecutionInfo.Execution.RunId, next.WorkflowExecutionInfo.Execution.RunId)
	draft, err := tenant.backend.Get("main", nil)
	require.NoError(t, err)
	draft.Version = 0
	draft.Steps = []model.Step{{ID: "pause", Type: "pause", Message: "Await cancel"}}
	_, err = tenant.backend.SaveDraft(draft)
	require.NoError(t, err)
	_, err = tenant.backend.Publish("main")
	require.NoError(t, err)
	require.NoError(t, f.directory.UpdateModule(ctx, "consumers", func(s *access.State) error {
		account := s.ServiceAccounts[a.ClientID]
		account.Grants[0].Versions = []int{1, 2}
		s.ServiceAccounts[a.ClientID] = account
		return nil
	}))
	body := startBody()
	body["version"] = 2
	response = f.call("POST", "/api/v1/runs", a.ClientID, "cancel-live", body)
	require.Equal(t, 202, response.Code, response.Body.String())
	id = responseID(t, response)
	response = f.call("POST", "/api/v1/runs/"+id+"/cancel", a.ClientID, "", nil)
	require.Equal(t, 202, response.Code, response.Body.String())
	require.Eventually(t, func() bool {
		response := f.call("GET", "/api/v1/runs/"+id, a.ClientID, "", nil)
		return response.Code == 200 && stringsContainsCanceled(response.Body.String())
	}, 10*time.Second, 100*time.Millisecond)
}
func stringsContainsCanceled(body string) bool {
	var value struct {
		Status string `json:"status"`
	}
	return json.Unmarshal([]byte(body), &value) == nil && value.Status == "canceled"
}
