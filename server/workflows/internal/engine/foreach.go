package engine

import (
	"fmt"

	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type iterationResult struct {
	index int
	value any
	err   error
}

func (r *interpreter) executeForEach(ctx workflow.Context, step model.Step, values *definition.Context) (any, error) {
	config := step.ForEach
	if config == nil || config.Concurrency < 1 || config.Concurrency > 10 || len(step.Branches) != 1 {
		return nil, temporal.NewNonRetryableApplicationError("invalid For Each configuration", "InvalidDefinition", nil)
	}
	raw, err := definition.Resolve(config.Items, values.Value())
	if err != nil {
		return nil, err
	}
	r.recordInput(ctx, raw)
	items, err := definition.DataItems(raw)
	if err != nil {
		return nil, err
	}
	// Bound history growth independently of data-operation limits. Nested loops
	// are rejected at publication; child workflows keep separate histories.
	if len(items) > 100 {
		return nil, temporal.NewNonRetryableApplicationError("For Each supports at most 100 items; split larger jobs into child runs", "IterationLimit", nil)
	}
	bodyCount := countIterationSteps(step.Branches[0].Steps)
	if bodyCount*len(items) > 1000 {
		return nil, temporal.NewNonRetryableApplicationError("For Each exceeds 1000 body-node executions", "IterationLimit", nil)
	}
	out := make([]any, len(items))
	for start := 0; start < len(items); start += config.Concurrency {
		end := start + config.Concurrency
		if end > len(items) {
			end = len(items)
		}
		batchCtx, cancel := workflow.WithCancel(ctx)
		results := workflow.NewBufferedChannel(ctx, end-start)
		for index := start; index < end; index++ {
			index := index
			workflow.Go(batchCtx, func(childCtx workflow.Context) {
				localSteps := map[string]any{}
				//workflowcheck:ignore copying values does not issue commands
				for k, v := range values.Steps {
					localSteps[k] = v
				}
				local := definition.Context{Input: values.Input, Signal: values.Signal, Steps: localSteps, Decision: values.Decision, Loop: map[string]any{"item": items[index], "index": index}}
				runner := *r
				runner.handlerRoot = false
				base := step.ID
				if r.handlerProc != nil && r.scope != "" {
					base = r.scope + "/" + base
				}
				runner.scope = fmt.Sprintf("%s/iteration/%d", base, index)
				err := runner.executeSteps(childCtx, step.Branches[0].Steps, &local, false)
				var result any
				if err == nil {
					result, err = definition.Resolve(config.Output, local.Value())
				}
				if err == nil {
					result, err = definition.DataResultBound(result)
				}
				results.Send(ctx, iterationResult{index, result, err})
			})
		}
		var failure error
		for index := start; index < end; index++ {
			var result iterationResult
			results.Receive(ctx, &result)
			if result.err != nil && failure == nil {
				failure = result.err
				cancel()
			}
			out[result.index] = result.value
		}
		cancel()
		if failure != nil {
			return nil, failure
		}
		// Abort before starting another batch if collected output exceeds limits.
		if _, err := definition.DataResultBound(out); err != nil {
			return nil, err
		}
	}
	return definition.DataResultBound(out)
}

func countIterationSteps(steps []model.Step) int {
	total := len(steps)
	for _, step := range steps {
		total += countIterationSteps(step.Then) + countIterationSteps(step.Else)
		multiplier := 1
		if step.Type == "decision" && step.Decision != nil {
			multiplier = step.Decision.MaxActions
		}
		for _, branch := range step.Branches {
			total += multiplier * countIterationSteps(branch.Steps)
		}
	}
	return total
}
