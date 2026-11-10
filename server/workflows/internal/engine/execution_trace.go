package engine

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/example/runloom/workflows/internal/model"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type traceContextKey struct{}

type executionTrace struct {
	model.ExecutionTrace
	payloadBytes int
}

func currentNode(ctx workflow.Context) *model.NodeExecution {
	node, _ := ctx.Value(traceContextKey{}).(*model.NodeExecution)
	return node
}

func (r *interpreter) beginNode(ctx workflow.Context, step model.Step) (workflow.Context, *model.NodeExecution) {
	if len(r.trace.Nodes) >= 1000 {
		r.trace.Omitted++
		return workflow.WithValue(ctx, traceContextKey{}, (*model.NodeExecution)(nil)), nil
	}
	node := &model.NodeExecution{Sequence: len(r.trace.Nodes) + 1, StepID: step.ID, Name: step.Name,
		Type: step.Type, Scope: r.scope, Status: "running", StartedAt: workflow.Now(ctx).Format(time.RFC3339Nano)}
	if parent := currentNode(ctx); parent != nil {
		node.Parent = parent.Sequence
	}
	r.trace.Nodes = append(r.trace.Nodes, node)
	return workflow.WithValue(ctx, traceContextKey{}, node), node
}

// Snapshot the resolved payload before later steps can mutate it. Limit both
// individual values and total response size independently of workflow outputs.
func (t *executionTrace) preview(value any) (json.RawMessage, bool) {
	if t.payloadBytes >= 384*1024 {
		return nil, true
	}
	//workflowcheck:ignore query-only JSON snapshots do not affect workflow commands
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 8*1024 || len(raw)+t.payloadBytes > 384*1024 {
		return nil, true
	}
	t.payloadBytes += len(raw)
	return raw, false
}

func (r *interpreter) recordInput(ctx workflow.Context, value any) {
	if n := currentNode(ctx); n != nil && n.Input == nil && !n.InputOmitted {
		n.Input, n.InputOmitted = r.trace.preview(value)
	}
}

func (r *interpreter) finishNode(ctx workflow.Context, n *model.NodeExecution, output any, err error) {
	if n == nil {
		return
	}
	n.FinishedAt = workflow.Now(ctx).Format(time.RFC3339Nano)
	if err == nil {
		n.Status = "completed"
		n.Output, n.OutputOmitted = r.trace.preview(output)
		return
	}
	n.Status = "failed"
	if temporal.IsCanceledError(err) || errors.Is(err, workflow.ErrCanceled) {
		n.Status = "cancelled"
	}
	n.Error = err.Error()
	if len(n.Error) > 512 {
		n.Error = n.Error[:512] + "…"
	}
}
