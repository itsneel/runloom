package engine

import (
	"testing"
	"time"

	"github.com/example/runloom/workflows/internal/model"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

func handlerDefinition() model.Definition {
	return model.Definition{ID: "handlers", SchemaVersion: "1.0", Name: "Handlers", Version: 1, Steps: []model.Step{{ID: "main-wait", Type: "wait", Seconds: 2}, {ID: "main-end", Type: "end", With: map[string]any{"main": true}}}, SignalHandlers: []model.SignalHandler{{ID: "customer-update", SignalName: "customer.updated", Steps: []model.Step{{ID: "handler-wait", Type: "wait", Seconds: 3}, {ID: "handler-return", Type: "end", With: map[string]any{"value": "${signal.payload.value}", "actor": "${signal.actor}"}}}}}}
}
func TestSignalHandlersRunAlongsideMainDrainAndDedupe(t *testing.T) {
	suite := testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	start := env.Now()
	id := "5f67b371-e86e-425d-bb0a-e05047c2581e"
	env.RegisterDelayedCallback(func() {
		event := model.WorkflowEvent{SignalName: "customer.updated", Payload: map[string]any{"value": "first"}, Actor: "alice", EventID: id}
		env.SignalWorkflow("signal", event)
		env.SignalWorkflow("signal", event)
	}, time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("customer.updated", map[string]any{"payload": map[string]any{"value": "second"}})
	}, 1500*time.Millisecond)
	env.RegisterDelayedCallback(func() {
		q, err := env.QueryWorkflow("status")
		require.NoError(t, err)
		var s model.RunState
		require.NoError(t, q.Get(&s))
		require.Equal(t, "main-end", s.CurrentStep.ID)
		require.Equal(t, 2, s.SignalHandlers["customer.updated"].Received)
		require.Equal(t, "running", s.SignalHandlers["customer.updated"].Status)
	}, 2500*time.Millisecond)
	env.ExecuteWorkflow(RunloomWorkflow, model.RunInput{Definition: handlerDefinition()})
	require.NoError(t, env.GetWorkflowError())
	var result model.RunResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, true, result.Output.(map[string]any)["main"])
	state := result.SignalHandlers["customer.updated"]
	require.Equal(t, 2, state.Completed)
	require.False(t, state.Accepting)
	require.Equal(t, "closed", state.Status)
	require.GreaterOrEqual(t, env.Now().Sub(start), 7*time.Second)
	require.Equal(t, "temporal-client", state.Output.(map[string]any)["actor"])
	require.Equal(t, "second", state.Output.(map[string]any)["value"])
}
func TestEarlyHandlerEventSurvivesImmediateMainEnd(t *testing.T) {
	suite := testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	d := handlerDefinition()
	d.Steps = d.Steps[1:]
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("customer.updated", map[string]any{"payload": map[string]any{"value": "early"}})
	}, 0)
	env.ExecuteWorkflow(RunloomWorkflow, model.RunInput{Definition: d})
	require.NoError(t, env.GetWorkflowError())
	var r model.RunResult
	require.NoError(t, env.GetWorkflowResult(&r))
	require.Equal(t, 1, r.SignalHandlers["customer.updated"].Completed)
}
func TestHandlerFailureCancelsMainAndConflictingEventIDFails(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "action failure", true: "event conflict"}[conflict], func(t *testing.T) {
			suite := testsuite.WorkflowTestSuite{}
			env := suite.NewTestWorkflowEnvironment()
			start := env.Now()
			d := handlerDefinition()
			d.Steps[0].Seconds = 86400
			if !conflict {
				d.SignalHandlers[0].Steps = []model.Step{{ID: "handler-fail", Type: "fail", Message: "Handler rejected action"}}
			}
			env.RegisterDelayedCallback(func() {
				event := model.WorkflowEvent{SignalName: "customer.updated", Payload: map[string]any{"value": "one"}, EventID: "5f67b371-e86e-425d-bb0a-e05047c2581e"}
				env.SignalWorkflow("signal", event)
				if conflict {
					event.Payload = map[string]any{"value": "two"}
					env.SignalWorkflow("signal", event)
				}
			}, time.Second)
			env.ExecuteWorkflow(RunloomWorkflow, model.RunInput{Definition: d})
			require.Error(t, env.GetWorkflowError())
			require.Less(t, env.Now().Sub(start), time.Hour)
		})
	}
}

func TestHandlerInvalidNativeEventsAndQueueOverflowFail(t *testing.T) {
	for _, kind := range []string{"missing payload", "unknown envelope field", "invalid event id", "queue overflow", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			suite := testsuite.WorkflowTestSuite{}
			env := suite.NewTestWorkflowEnvironment()
			d := handlerDefinition()
			d.Steps[0].Seconds = 86400
			env.RegisterDelayedCallback(func() {
				switch kind {
				case "missing payload":
					env.SignalWorkflow("customer.updated", map[string]any{"event_id": "x"})
				case "unknown envelope field":
					env.SignalWorkflow("customer.updated", map[string]any{"payload": nil, "actor": "forged"})
				case "invalid event id":
					env.SignalWorkflow("customer.updated", map[string]any{"payload": nil, "event_id": "invalid"})
				case "queue overflow":
					for i := 0; i < maxPendingHandlerEvents+2; i++ {
						env.SignalWorkflow("customer.updated", map[string]any{"payload": i})
					}
				case "cancel":
					env.CancelWorkflow()
				}
			}, time.Second)
			start := env.Now()
			env.ExecuteWorkflow(RunloomWorkflowWithSignalHandlers, model.RunInput{Definition: d})
			require.Error(t, env.GetWorkflowError())
			require.Less(t, env.Now().Sub(start), time.Hour)
		})
	}
}
