package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/converter"
)

type handlerTestClient struct {
	*documentedClient
	state  model.RunState
	events []model.WorkflowEvent
}

func (c *handlerTestClient) QueryWorkflow(context.Context, string, string, string, ...any) (converter.EncodedValue, error) {
	p, _ := converter.GetDefaultDataConverter().ToPayload(c.state)
	return executionValue{payload: p}, nil
}
func (c *handlerTestClient) SignalWorkflow(_ context.Context, _ string, _ string, _ string, event any) error {
	c.events = append(c.events, event.(model.WorkflowEvent))
	return nil
}
func TestSignalHandlerDeliveryChecksSchemaAndPreservesTrustedActor(t *testing.T) {
	b, err := store.New(t.TempDir())
	require.NoError(t, err)
	c := &handlerTestClient{documentedClient: &documentedClient{}, state: model.RunState{SignalHandlers: map[string]model.SignalHandlerState{"customer.updated": {Accepting: true, InputSchema: map[string]any{"type": "object", "required": []any{"customer_id"}}}}}}
	s := New(b, c, "queue")
	defer s.Close()
	call := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/studio/runs/run/signals/customer.updated", strings.NewReader(body))
		r.Header.Set("X-Runloom-Actor", "verified-editor")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	id := "5f67b371-e86e-425d-bb0a-e05047c2581e"
	invalid := call(`{"event_id":"` + id + `","payload":{}}`)
	require.Equal(t, 422, invalid.Code)
	require.Empty(t, c.events)
	accepted := call(`{"event_id":"` + id + `","actor":"spoofed","payload":{"customer_id":"synthetic"}}`)
	require.Equal(t, 200, accepted.Code, accepted.Body.String())
	require.Len(t, c.events, 1)
	require.Equal(t, "verified-editor", c.events[0].Actor)
	require.Equal(t, id, c.events[0].EventID)
	state := c.state.SignalHandlers["customer.updated"]
	state.Accepting = false
	c.state.SignalHandlers["customer.updated"] = state
	closed := call(`{"event_id":"` + id + `","payload":{"customer_id":"synthetic"}}`)
	require.Equal(t, 409, closed.Code)
	require.Len(t, c.events, 1)
	_, err = json.Marshal(c.events)
	require.NoError(t, err)
}
