package engine

import (
	"errors"
	"github.com/example/runloom/workflows/internal/budget"
	"github.com/example/runloom/workflows/internal/model"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
	"time"
)

func (r *interpreter) executeBudgetedLLM(ctx workflow.Context, step model.Step, payload any) (any, error) {
	enabled := workflow.GetVersion(ctx, "llm-budget-pause-v1", workflow.DefaultVersion, 1) != workflow.DefaultVersion
	if fields, ok := payload.(map[string]any); ok && enabled {
		fields["_runloom_budget_v1"] = true
	}
	defer func() { delete(r.budgetPauses, step.ID); delete(r.state.PausedSteps, step.ID) }()
	for {
		var output any
		err := workflow.ExecuteActivity(ctx, "runloom.llm", payload).Get(ctx, &output)
		var appErr *temporal.ApplicationError
		if enabled && err == nil {
			if fields, ok := output.(map[string]any); ok {
				if raw, present := fields["_runloom_budget_pause"]; present {
					decision := budgetDecision(raw)
					delete(fields, "_runloom_budget_pause")
					checkCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: 15 * time.Second, RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1}})
					for {
						if err := r.awaitBudgetResume(ctx, step.ID, decision); err != nil {
							return nil, err
						}
						var next *budget.Decision
						if err := workflow.ExecuteActivity(checkCtx, "runloom.internal.check_llm_budget").Get(checkCtx, &next); err != nil {
							if temporal.IsCanceledError(err) {
								return nil, err
							}
							decision = budget.Unavailable(decision.ProjectID)
							continue
						}
						if next == nil {
							return output, nil
						}
						decision = *next
					}
				}
			}
		}
		if !enabled || !errors.As(err, &appErr) || appErr.Type() != budget.PauseError {
			return output, err
		}
		decision := budget.Unavailable("")
		if appErr.HasDetails() {
			_ = appErr.Details(&decision)
		}
		if err = r.awaitBudgetResume(ctx, step.ID, decision); err != nil {
			return nil, err
		}
		// Keep the barrier while the same LLM node rechecks current policy/spend.
		// Only a rejected, never-dispatched request is attempted again here.
	}
}

// Activity output is decoded into plain JSON values. Copy known scalar fields
// directly so workflow logic does not depend on reflection/JSON serialization.
func budgetDecision(raw any) budget.Decision {
	if d, ok := raw.(budget.Decision); ok {
		return d
	}
	fields, ok := raw.(map[string]any)
	if !ok {
		return budget.Unavailable("")
	}
	text := func(key string) string { v, _ := fields[key].(string); return v }
	revision, _ := fields["policy_revision"].(float64)
	incomplete, _ := fields["incomplete"].(bool)
	return budget.Decision{PolicyID: text("policy_id"), PolicyName: text("policy_name"), PolicyRevision: int64(revision), ProjectID: text("project_id"), ProjectName: text("project_name"), Month: text("month"), ResetsAt: text("resets_at"), SpendUSD: text("spend_usd"), LimitUSD: text("limit_usd"), Incomplete: incomplete, Reason: text("reason"), Message: text("message")}
}

func (r *interpreter) awaitBudgetResume(ctx workflow.Context, step string, decision budget.Decision) error {
	gate := model.PauseGate{StepID: step, Message: decision.Message, Budget: &decision}
	r.budgetPauses[step] = gate
	r.state.PausedSteps[step] = gate
	workflow.GetLogger(ctx).Warn("Workflow paused by LLM budget policy", "step", step, "reason", decision.Reason, "policy", decision.PolicyID)
	delete(r.resumes, step)
	if err := workflow.Await(ctx, func() bool { _, ok := r.resumes[step]; return ok }); err != nil {
		return err
	}
	delete(r.resumes, step)
	return nil
}
