package activities

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/example/runloom/workflows/internal/budget"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/example/runloom/workflows/internal/usage"
	"github.com/google/uuid"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
)

type MeteredLLM struct {
	OpenTeam  func(context.Context, string) (MeteringStore, error)
	Ancestors func(context.Context, string, string) ([]string, error)
	Prices    usage.Catalogue
	Provider  string
}

type MeteringStore interface {
	store.BudgetReader
	store.Audits
	GetRun(string) (store.RunRecord, error)
	Close(context.Context) error
}

func (a *MeteredLLM) Call(ctx context.Context, payload map[string]any) (map[string]any, error) {
	delete(payload, "_runloom_budget_pause") // Never trust workflow-supplied result metadata.
	info := activity.GetInfo(ctx)
	output, err := callLLM(ctx, payload, func(ctx context.Context, model string) (func(llmObservation) bool, error) {
		finish, err := a.begin(ctx, payload, model, info)
		var appErr *temporal.ApplicationError
		if err != nil && payload["_runloom_budget_v1"] == true && !errors.As(err, &appErr) {
			d := budget.Unavailable("")
			return nil, temporal.NewNonRetryableApplicationError(d.Message, budget.PauseError, nil, d)
		}
		return finish, err
	})
	if output != nil && payload["_runloom_budget_v1"] == true {
		if d, ok := payload["_runloom_budget_pause"].(budget.Decision); ok {
			output["_runloom_budget_pause"] = d
		}
	}
	return output, err
}

func (a *MeteredLLM) begin(ctx context.Context, payload map[string]any, model string, info activity.Info) (func(llmObservation) bool, error) {
	id := info.WorkflowExecution.ID
	if len(id) < 37 || id[36] != '-' {
		return nil, temporal.NewNonRetryableApplicationError("LLM execution has no valid team identity", "InvalidTeam", nil)
	}
	team := id[:36]
	parsed, err := uuid.Parse(team)
	if err != nil || parsed.String() != team {
		return nil, temporal.NewNonRetryableApplicationError("invalid LLM team identity", "InvalidTeam", nil)
	}
	b, err := a.OpenTeam(ctx, team)
	if err != nil {
		return nil, fmt.Errorf("LLM accounting storage unavailable before request")
	}
	closeStore := func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = b.Close(closeCtx)
	}
	run, err := b.GetRun(id)
	if err != nil || run.TemporalRunID != info.WorkflowExecution.RunID {
		closeStore()
		return nil, fmt.Errorf("LLM execution record unavailable before request")
	}
	ancestors, err := a.Ancestors(ctx, id, info.WorkflowExecution.RunID)
	if err != nil {
		closeStore()
		return nil, fmt.Errorf("LLM run ancestry unavailable before request")
	}
	step, _ := payload["_runloom_step_id"].(string)
	if step == "" || len(step) > 64 {
		step = "activity:" + info.ActivityID
	}
	started := time.Now().UTC()
	project := ""
	if payload["_runloom_budget_v1"] == true {
		checkCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		var decisions []budget.Decision
		project, decisions, err = store.CheckLLMBudget(checkCtx, b, run.DefinitionID, started)
		cancel()
		if err != nil {
			closeStore()
			d := budget.Unavailable(project)
			return nil, temporal.NewNonRetryableApplicationError(d.Message, budget.PauseError, nil, d)
		}
		for _, d := range decisions {
			a.logBudget(b, id, run.DefinitionID, step, d)
			if d.Reason == "hard_limit_reached" {
				closeStore()
				return nil, temporal.NewNonRetryableApplicationError(d.Message, budget.PauseError, nil, d)
			}
		}
	}
	entry := usage.Entry{ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("%s:%s:%s:%d", info.WorkflowNamespace, info.WorkflowExecution.RunID, info.ActivityID, info.Attempt))).String(), TeamID: team, WorkflowID: id, TemporalRunID: info.WorkflowExecution.RunID, Ancestors: ancestors, DefinitionID: run.DefinitionID, DefinitionVersion: run.DefinitionVersion, StepID: step, ActivityID: info.ActivityID, Attempt: info.Attempt, IsTest: run.TriggerType == "child_test" || run.TriggerType == "draft_test" || (run.State != nil && run.State.IsTest), Provider: a.Provider, RequestedModel: model, Model: model, StartedAt: started.Format(time.RFC3339Nano), Status: "pending", Accounting: "pending"}
	entry.ProjectID = project
	created, err := b.BeginUsage(ctx, entry)
	if err != nil {
		closeStore()
		return nil, fmt.Errorf("LLM accounting could not record request intent; provider was not called")
	}
	if !created {
		closeStore()
		return nil, temporal.NewNonRetryableApplicationError("LLM attempt already recorded; not repeating a potentially paid request", "DuplicateLLMAttempt", nil)
	}
	return func(observation llmObservation) bool {
		defer closeStore()
		entry.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		entry.DurationMS = time.Since(started).Milliseconds()
		entry.Status = "failed"
		if observation.Success {
			entry.Status = "succeeded"
		}
		entry.ProviderRequestID = safeMetadata(observation.RequestID, 200)
		if resolved, ok := observation.Response["model"].(string); ok && resolved != "" {
			entry.Model = safeMetadata(resolved, 200)
		}
		raw, _ := observation.Response["usage"].(map[string]any)
		a.Prices.Price(&entry, raw)
		// Accounting failure must never cause another paid provider request. A
		// durable pending intent remains visible if this bounded write fails.
		writeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := b.FinishUsage(writeCtx, entry); err != nil {
			log.Print("LLM usage completion could not be persisted; pending accounting needs reconciliation")
			return false
		}
		if observation.Decision != nil {
			_, err := b.AppendAudit(store.AuditEvent{EventID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(entry.ID+":decision")).String(), WorkflowID: id, DefinitionID: run.DefinitionID, StepID: step, EventType: "llm.decision_selected", Actor: "system:ai-decision", Payload: map[string]any{"usage_id": entry.ID, "activity_id": info.ActivityID, "attempt": info.Attempt, "decision": *observation.Decision}})
			if err != nil {
				log.Print("Decision audit persistence unavailable; decision remains in Temporal history")
			}
		}
		// Record threshold crossings even when this is the run's last LLM request.
		// This check never retries or discards the provider's completed response.
		if payload["_runloom_budget_v1"] == true {
			checkCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_, decisions, checkErr := store.CheckLLMBudget(checkCtx, b, run.DefinitionID, time.Now().UTC())
			cancel()
			if checkErr == nil {
				for _, d := range decisions {
					a.logBudget(b, id, run.DefinitionID, step, d)
					if d.Reason == "hard_limit_reached" {
						payload["_runloom_budget_pause"] = d
					}
				}
			} else {
				payload["_runloom_budget_pause"] = budget.Unavailable(project)
			}
		}
		return true
	}, nil
}

// Check resumes a post-response budget checkpoint without making another paid call.
func (a *MeteredLLM) Check(ctx context.Context) (*budget.Decision, error) {
	info := activity.GetInfo(ctx)
	id := info.WorkflowExecution.ID
	if len(id) < 37 {
		d := budget.Unavailable("")
		return &d, nil
	}
	b, err := a.OpenTeam(ctx, id[:36])
	if err != nil {
		d := budget.Unavailable("")
		return &d, nil
	}
	defer b.Close(ctx)
	run, err := b.GetRun(id)
	if err != nil || run.TemporalRunID != info.WorkflowExecution.RunID {
		d := budget.Unavailable("")
		return &d, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	project, decisions, err := store.CheckLLMBudget(ctx, b, run.DefinitionID, time.Now().UTC())
	if err != nil {
		d := budget.Unavailable(project)
		return &d, nil
	}
	for _, d := range decisions {
		if d.Reason == "hard_limit_reached" {
			return &d, nil
		}
	}
	return nil, nil
}

func (a *MeteredLLM) logBudget(b store.Audits, run, definition, step string, d budget.Decision) {
	log.Printf("LLM budget policy event: reason=%s policy=%s project=%s workflow=%s", d.Reason, d.PolicyID, d.ProjectID, run)
	if _, err := b.AppendAudit(store.AuditEvent{WorkflowID: run, DefinitionID: definition, StepID: step, EventType: "llm_budget." + d.Reason, Actor: "system:llm-budget", Payload: d}); err != nil {
		log.Print("LLM budget audit persistence unavailable; consult worker logs and active budget checkpoints")
	}
}

func safeMetadata(value string, max int) string {
	value = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, value)
	if len(value) > max {
		value = value[:max]
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
	}
	return value
}

// The Temporal service supplies lineage, not user-controlled workflow payloads.
func LLMAncestors(c client.Client) func(context.Context, string, string) ([]string, error) {
	return func(ctx context.Context, id, run string) ([]string, error) {
		result := []string{}
		teamPrefix := ""
		if len(id) >= 37 {
			teamPrefix = id[:37]
		}
		for depth := 0; depth < 32; depth++ {
			if teamPrefix == "" || !strings.HasPrefix(id, teamPrefix) {
				return nil, fmt.Errorf("cross-team ancestry")
			}
			result = append(result, usage.RunKey(id, run))
			info, err := c.DescribeWorkflowExecution(ctx, id, run)
			if err != nil {
				return nil, err
			}
			if info == nil || info.WorkflowExecutionInfo == nil {
				return nil, fmt.Errorf("missing Temporal run description")
			}
			parent := info.WorkflowExecutionInfo.GetParentExecution()
			if parent == nil {
				return result, nil
			}
			id, run = parent.WorkflowId, parent.RunId
		}
		return nil, fmt.Errorf("LLM run ancestry exceeds limit")
	}
}
