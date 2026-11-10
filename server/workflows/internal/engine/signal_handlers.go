package engine

import (
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/google/uuid"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const maxHandlerEvents = 1000
const maxPendingHandlerEvents = 100
const maxHandlerPayloadBytes = 64 * 1024

type handlerEvent struct {
	handler model.SignalHandler
	event   model.WorkflowEvent
}
type handlerChannel struct {
	name    string
	channel workflow.ReceiveChannel
}
type signalHandlers struct {
	runner                *interpreter
	definitions           map[string]model.SignalHandler
	channels              []handlerChannel
	queue                 []handlerEvent
	seen                  map[string][32]byte
	received              int
	active, ready, closed bool
	failure               error
	cancelMain            workflow.CancelFunc
}

func newSignalHandlers(ctx workflow.Context, runner *interpreter, cancel workflow.CancelFunc) *signalHandlers {
	p := &signalHandlers{runner: runner, definitions: map[string]model.SignalHandler{}, seen: map[string][32]byte{}, cancelMain: cancel}
	runner.state.SignalHandlers = map[string]model.SignalHandlerState{}
	for _, handler := range runner.input.Definition.SignalHandlers {
		p.definitions[handler.SignalName] = handler
		runner.state.SignalHandlers[handler.SignalName] = model.SignalHandlerState{ID: handler.ID, SignalName: handler.SignalName, Status: "listening", Accepting: true, InputSchema: handler.InputSchema}
		channel := workflow.GetSignalChannel(ctx, handler.SignalName)
		p.channels = append(p.channels, handlerChannel{handler.SignalName, channel})
		name := handler.SignalName
		workflow.Go(ctx, func(ctx workflow.Context) {
			for {
				var raw any
				if !channel.Receive(ctx, &raw) {
					return
				}
				p.native(name, raw)
			}
		})
	}
	workflow.Go(ctx, func(ctx workflow.Context) {
		p.ready = true
		for {
			if err := workflow.Await(ctx, func() bool { return p.closed || p.failure != nil || len(p.queue) > 0 }); err != nil {
				return
			}
			if p.closed || p.failure != nil {
				return
			}
			next := p.queue[0]
			p.queue = p.queue[1:]
			p.active = true
			state := runner.state.SignalHandlers[next.handler.SignalName]
			state.Status = "running"
			state.Pending--
			state.InvocationID = fmt.Sprintf("%s-%d", next.handler.ID, state.Completed+1)
			runner.state.SignalHandlers[next.handler.SignalName] = state
			local := definition.Context{Input: runner.input.Input, Steps: map[string]any{}, Signal: map[string]any{"payload": next.event.Payload, "actor": next.event.Actor, "name": next.event.SignalName, "event_id": next.event.EventID}}
			child := *runner
			child.handlerRoot = true
			child.ended = false
			child.endOutput = nil
			child.scope = "signal-handler/" + state.InvocationID
			var err error
			if len(next.handler.InputSchema) > 0 {
				validation := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: 15 * time.Second, RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1}})
				err = workflow.ExecuteActivity(validation, "runloom.internal.validate_output", map[string]any{"schema": next.handler.InputSchema, "output": next.event.Payload}).Get(validation, nil)
			}
			if err == nil {
				err = child.executeSteps(ctx, next.handler.Steps, &local, false)
			}
			state = runner.state.SignalHandlers[next.handler.SignalName]
			if err != nil {
				state.Status = "failed"
				state.Error = err.Error()
				runner.state.SignalHandlers[next.handler.SignalName] = state
				p.active = false
				p.fail(fmt.Errorf("signal handler %s failed: %w", next.handler.ID, err))
				return
			}
			state.Output = child.endOutput
			if !child.ended {
				state.Output = local.Steps
			}
			state.Completed++
			state.Status = "listening"
			if state.Pending > 0 {
				state.Status = "queued"
			}
			runner.state.SignalHandlers[next.handler.SignalName] = state
			p.active = false
		}
	})
	return p
}
func (p *signalHandlers) fail(err error) {
	if p.failure == nil {
		p.failure = err
		p.cancelMain()
	}
}
func (p *signalHandlers) native(name string, raw any) {
	envelope, ok := raw.(map[string]any)
	if !ok {
		p.fail(temporal.NewNonRetryableApplicationError("native signals require a JSON object with payload and optional event_id", "InvalidHandlerEvent", nil))
		return
	}
	payload, present := envelope["payload"]
	if !present {
		p.fail(temporal.NewNonRetryableApplicationError("native signal payload is required", "InvalidHandlerEvent", nil))
		return
	}
	unknown := false
	//workflowcheck:ignore pure membership validation; order cannot affect the result
	for key := range envelope {
		if key != "payload" && key != "event_id" {
			unknown = true
		}
	}
	if unknown {
		p.fail(temporal.NewNonRetryableApplicationError("native signal envelope supports only payload and event_id", "InvalidHandlerEvent", nil))
		return
	}
	eventID := ""
	if value, exists := envelope["event_id"]; exists {
		var valid bool
		eventID, valid = value.(string)
		if !valid {
			p.fail(temporal.NewNonRetryableApplicationError("event_id must be a UUID string", "InvalidHandlerEvent", nil))
			return
		}
	}
	p.enqueue(model.WorkflowEvent{SignalName: name, Payload: payload, EventID: eventID, Actor: "temporal-client"})
}
func (p *signalHandlers) enqueue(event model.WorkflowEvent) bool {
	handler, known := p.definitions[event.SignalName]
	if !known {
		return false
	}
	if p.closed || p.failure != nil {
		return true
	}
	raw, err := definition.CanonicalDataJSON(event.Payload)
	if err != nil || len(raw) > maxHandlerPayloadBytes {
		p.fail(temporal.NewNonRetryableApplicationError("handler payload must be JSON no larger than 64 KiB", "InvalidHandlerEvent", nil))
		return true
	}
	if event.EventID != "" {
		id, err := uuid.Parse(event.EventID)
		if err != nil {
			p.fail(temporal.NewNonRetryableApplicationError("event_id must be a UUID", "InvalidHandlerEvent", nil))
			return true
		}
		event.EventID = id.String()
		key := handler.ID + "/" + event.EventID
		digest := sha256.Sum256(append([]byte(event.Actor+"\n"), raw...))
		if old, seen := p.seen[key]; seen {
			if old != digest {
				p.fail(temporal.NewNonRetryableApplicationError("event_id was reused with a different payload or sender", "HandlerEventConflict", nil))
			}
			return true
		}
		p.seen[key] = digest
	}
	if p.received >= maxHandlerEvents || len(p.queue) >= maxPendingHandlerEvents {
		p.fail(temporal.NewNonRetryableApplicationError("signal handler event or pending queue limit exceeded", "HandlerEventLimit", nil))
		return true
	}
	p.received++
	p.queue = append(p.queue, handlerEvent{handler, event})
	state := p.runner.state.SignalHandlers[event.SignalName]
	state.Received++
	state.Pending++
	if state.Status != "running" {
		state.Status = "queued"
	}
	p.runner.state.SignalHandlers[event.SignalName] = state
	return true
}
func (p *signalHandlers) finish(ctx workflow.Context, generic workflow.ReceiveChannel) error {
	// Drain already buffered messages before testing quiescence. No yield occurs
	// between the final empty check and workflow completion.
	for {
		var event model.WorkflowEvent
		for generic.ReceiveAsync(&event) {
			if !p.enqueue(event) {
				p.runner.signals[event.SignalName] = event
			}
		}
		for _, entry := range p.channels {
			var raw any
			for entry.channel.ReceiveAsync(&raw) {
				p.native(entry.name, raw)
			}
		}
		if p.failure != nil {
			return p.failure
		}
		if len(p.queue) == 0 && !p.active {
			break
		}
		if err := workflow.Await(ctx, func() bool { return p.failure != nil || (len(p.queue) == 0 && !p.active) }); err != nil {
			return err
		}
	}
	p.closed = true
	//workflowcheck:ignore status-only updates issue no commands
	for name, state := range p.runner.state.SignalHandlers {
		state.Status = "closed"
		state.Accepting = false
		p.runner.state.SignalHandlers[name] = state
	}
	return nil
}

func RunloomWorkflowWithSignalHandlers(ctx workflow.Context, input model.RunInput) (model.RunResult, error) {
	return RunloomWorkflow(ctx, input)
}
func RunloomDraftTestWorkflowWithSignalHandlers(ctx workflow.Context, input model.RunInput) (model.RunResult, error) {
	return RunloomDraftTestWorkflow(ctx, input)
}
func RunloomChildTestWorkflowWithSignalHandlers(ctx workflow.Context, input model.RunInput) (model.RunResult, error) {
	return RunloomChildTestWorkflow(ctx, input)
}
