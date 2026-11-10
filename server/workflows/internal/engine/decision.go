package engine

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"time"

	"github.com/example/runloom/workflows/internal/decision"
	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// A stable per-invocation ID prevents old replies or child IDs being reused in
// another agent round. Definition-local output references retain their names.
func scopedStepID(scope, id string) string {
	sum := sha256.Sum256([]byte(scope + "/" + id))
	return fmt.Sprintf("action-%x", sum[:20])
}

func (r *interpreter) executeDecision(ctx workflow.Context, step model.Step, values *definition.Context) (any, error) {
	c := step.Decision
	if c == nil {
		return nil, fmt.Errorf("decision configuration is required")
	}
	hitlContract := workflow.GetVersion(ctx, "decision-hitl-contract-v1", workflow.DefaultVersion, 1) != workflow.DefaultVersion
	ids, catalogue := []string{}, []any{}
	for _, b := range step.Branches {
		ids = append(ids, b.ID)
		catalogue = append(catalogue, map[string]any{"id": b.ID, "description": b.Description})
	}
	rounds := []any{}
	used := 0
	result := map[string]any{"mode": c.Mode, "rounds": rounds, "actions_executed": used, "outcome": "running"}
	checkpoint := func() { r.state.Steps[step.ID] = model.StepState{Status: "running", Output: result} }
	for round := 1; round <= c.MaxRounds; round++ {
		if err := workflow.Await(ctx, func() bool { return len(r.budgetPauses) == 0 }); err != nil {
			return nil, err
		}
		payload, err := definition.Resolve(map[string]any{"model": step.Model, "system_prompt": step.SystemPrompt, "prompt": step.Prompt, "temperature": *step.Temperature, "max_tokens": *step.MaxTokens}, values.Value())
		if err != nil {
			return nil, err
		}
		fields := payload.(map[string]any)
		if err := promptPayload(step, values, fields); err != nil {
			return nil, err
		}
		r.recordInput(ctx, fields)
		fields["_runloom_step_id"] = step.ID
		fields["_runloom_decision"] = map[string]any{"mode": c.Mode, "remaining": c.MaxActions - used, "actions": catalogue, "history": rounds}
		retry := step.Retry
		activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Duration(*step.TimeoutSeconds) * time.Second, ScheduleToCloseTimeout: time.Duration(*step.TimeoutSeconds*retry.MaximumAttempts+120) * time.Second, RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: int32(retry.MaximumAttempts), InitialInterval: time.Duration(retry.InitialInterval * float64(time.Second)), BackoffCoefficient: retry.BackoffCoefficient, MaximumInterval: time.Duration(retry.MaximumInterval * float64(time.Second))}})
		checkpoint()
		output, err := r.executeBudgetedLLM(activityCtx, step, fields)
		if err != nil {
			return nil, err
		}
		response, _ := output.(map[string]any)
		// Recorded pre-HITL decisions predate the required fields. Only replay
		// of that version may interpret the old handoff outcome as review.
		if !hitlContract {
			if data, ok := response["data"].(map[string]any); ok && len(data) == 3 {
				required := data["outcome"] == "handoff"
				reason := ""
				if required {
					reason, _ = data["reason"].(string)
				}
				response["data"] = map[string]any{"outcome": data["outcome"], "actions": data["actions"], "reason": data["reason"], "hitl_required": required, "hitl_reason": reason}
			}
		}
		choice, err := decision.FromValue(response["data"])
		if err == nil {
			err = decision.Validate(choice, ids, c.Mode, c.MaxActions-used)
		}
		if err != nil {
			return nil, temporal.NewNonRetryableApplicationError(err.Error(), "InvalidDecisionResponse", nil)
		}
		record := map[string]any{"round": round, "outcome": choice.Outcome, "actions": choice.Actions, "reason": choice.Reason, "results": map[string]any{}, "model": response["model"], "usage": response["usage"]}
		if hitlContract {
			record["hitl_required"], record["hitl_reason"] = choice.Required, choice.Review.Reason
			result["hitl_required"], result["hitl_reason"] = choice.Required, choice.Review.Reason
		}
		rounds = append(rounds, record)
		result["rounds"] = rounds
		result["selected_actions"], result["reason"], result["results"] = choice.Actions, choice.Reason, record["results"]
		checkpoint()
		if choice.Outcome == "finish" {
			result["outcome"] = "finished"
			return result, nil
		}
		if choice.Outcome == "handoff" {
			result["outcome"] = "handoff"
			// A handoff resume explicitly continues after this node; it never
			// asks the model again or silently approves a proposed action.
			delete(r.resumes, step.ID)
			reason := choice.Reason
			if hitlContract {
				reason = choice.Review.Reason
			}
			_, err := r.waitForResume(ctx, model.Step{ID: step.ID, Message: "AI Decision requests human review: " + reason}, values, false)
			if err != nil {
				return nil, err
			}
			result["outcome"] = "handoff_resumed"
			return result, nil
		}
		actionCtx, cancel := workflow.WithCancel(ctx)
		ch := workflow.NewBufferedChannel(ctx, len(choice.Actions))
		run := func(actionCtx workflow.Context, index int, id string) {
			var branch model.Branch
			for _, b := range step.Branches {
				if b.ID == id {
					branch = b
					break
				}
			}
			localSteps := map[string]any{}
			//workflowcheck:ignore copying data does not issue commands
			for key, v := range values.Steps {
				localSteps[key] = v
			}
			local := definition.Context{Input: values.Input, Signal: values.Signal, Steps: localSteps, Loop: values.Loop, Decision: map[string]any{"round": round, "reason": choice.Reason, "previous": rounds[:len(rounds)-1]}}
			actionRunner := *r
			actionRunner.handlerRoot = false
			base := step.ID
			if r.handlerProc != nil && r.scope != "" {
				base = r.scope + "/" + base
			}
			actionRunner.scope = fmt.Sprintf("%s/%d/%s", base, round, id)
			err := actionRunner.executeSteps(actionCtx, branch.Steps, &local, false)
			additions := map[string]any{}
			//workflowcheck:ignore output map construction does not issue commands
			for key, v := range local.Steps {
				if _, existed := values.Steps[key]; !existed {
					additions[key] = v
				}
			}
			ch.Send(ctx, branchOutcome{Index: index, ID: id, Steps: additions, Err: err})
		}
		if c.Execution == "parallel" {
			for i, id := range choice.Actions {
				i, id := i, id
				workflow.Go(actionCtx, func(inner workflow.Context) { run(inner, i, id) })
			}
		} else {
			for i, id := range choice.Actions {
				run(actionCtx, i, id)
				var o branchOutcome
				ch.Receive(ctx, &o)
				record["results"].(map[string]any)[id] = o.Steps
				if o.Err != nil {
					cancel()
					return nil, o.Err
				}
				used++
				result["actions_executed"] = used
			}
		}
		if c.Execution == "parallel" {
			outcomes := []branchOutcome{}
			var firstErr error
			for range choice.Actions {
				var o branchOutcome
				ch.Receive(ctx, &o)
				outcomes = append(outcomes, o)
				if o.Err != nil {
					if firstErr == nil {
						firstErr = o.Err
					}
					cancel()
				}
			}
			sort.Slice(outcomes, func(i, j int) bool { return outcomes[i].Index < outcomes[j].Index })
			for _, o := range outcomes {
				record["results"].(map[string]any)[o.ID] = o.Steps
				if o.Err == nil {
					used++
				}
			}
			result["actions_executed"] = used
			if firstErr != nil {
				cancel()
				return nil, firstErr
			}
		}
		cancel()
		if c.Mode != "agent" {
			result["outcome"] = "completed"
			return result, nil
		}
	}
	return nil, temporal.NewNonRetryableApplicationError("AI Decision reached its round limit before finishing", "DecisionLimitExceeded", nil)
}
