// Package rexeralien provides the TKT0020 boundary activities. Business source
// revisions and tag/QA eligibility belong to R2; orchestration belongs to the
// published Runloom definition. This package never executes QA or releases files.
package rexeralien

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
)

type Activities struct{ Client *Client }

func (a *Activities) Register(w worker.Worker) {
	w.RegisterActivityWithOptions(a.Read, activity.RegisterOptions{Name: "rexera.lien.read"})
	w.RegisterActivityWithOptions(a.Classify, activity.RegisterOptions{Name: "rexera.lien.classify"})
	w.RegisterActivityWithOptions(a.Save, activity.RegisterOptions{Name: "rexera.lien.save"})
	w.RegisterActivityWithOptions(a.PlanQA, activity.RegisterOptions{Name: "rexera.lien.plan-qa"})
	w.RegisterActivityWithOptions(a.SubmitQA, activity.RegisterOptions{Name: "rexera.lien.submit-qa"})
	w.RegisterActivityWithOptions(a.Finish, activity.RegisterOptions{Name: "rexera.lien.finish"})
}

func (a *Activities) authorized(ctx context.Context) error {
	if a.Client == nil || a.Client.TeamID == "" || !strings.HasPrefix(activity.GetInfo(ctx).WorkflowExecution.ID, a.Client.TeamID+"-") {
		return temporal.NewNonRetryableApplicationError("lien integration is not configured for this team", "LienTeamDenied", nil)
	}
	return nil
}

func object(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}
func text(m map[string]any, key string) string    { v, _ := m[key].(string); return v }
func boolValue(m map[string]any, key string) bool { v, _ := m[key].(bool); return v }
func sourcePath(source map[string]any, suffix string) (string, error) {
	id := text(source, "document_id")
	if _, err := uuid.Parse(id); err != nil {
		return "", temporal.NewNonRetryableApplicationError("invalid document identity", "LienInputInvalid", nil)
	}
	return "/api/documents/lien-processing/" + id + suffix, nil
}
func runIdentity(ctx context.Context) map[string]any {
	info := activity.GetInfo(ctx)
	return map[string]any{"run_id": info.WorkflowExecution.ID, "temporal_run_id": info.WorkflowExecution.RunID}
}
func failedClassification(code string) map[string]any {
	return map[string]any{"status": "failed", "requires_human_review": true, "error_code": code}
}

func (a *Activities) Read(ctx context.Context, input map[string]any) (map[string]any, error) {
	if err := a.authorized(ctx); err != nil {
		return nil, err
	}
	if _, err := uuid.Parse(text(input, "event_id")); err != nil {
		return nil, temporal.NewNonRetryableApplicationError("invalid event identity", "LienInputInvalid", nil)
	}
	path, err := sourcePath(input, "/read")
	if err != nil {
		return nil, err
	}
	return a.Client.R2(ctx, path, map[string]any{"event_id": input["event_id"], "run": runIdentity(ctx)})
}

func (a *Activities) Classify(ctx context.Context, source map[string]any) (map[string]any, error) {
	if err := a.authorized(ctx); err != nil {
		return nil, err
	}
	if !boolValue(source, "needs_classification") {
		return map[string]any{"status": "bypassed", "requires_human_review": false}, nil
	}
	path, err := sourcePath(source, "/pdf")
	if err != nil {
		return nil, err
	}
	pdf, err := a.Client.R2(ctx, path, map[string]any{"event_id": source["event_id"], "revision": source["revision"]})
	if statusCode(err) == 409 {
		return map[string]any{"status": "superseded", "requires_human_review": false}, nil
	}
	if err == nil {
		pdfURL := text(pdf, "pdf_url")
		if pdfURL == "" {
			return failedClassification("pdf_unavailable"), nil
		}
		var result map[string]any
		result, err = a.Client.Classifier(ctx, map[string]any{"pdf_url": pdfURL, "order_id": pdf["order_id"], "order_type": "LIEN"})
		if err == nil {
			// Unknown/malformed outputs cannot silently become confident tags.
			review, ok := result["requires_human_review"].(bool)
			if !ok {
				review = true
			}
			var primary any
			if value, valid := result["final_tag"].(string); valid {
				primary = value
			} else {
				review = true
			}
			return map[string]any{"status": "classified", "final_tag": primary, "requires_human_review": review}, nil
		}
	}
	if statusCode(err) == 400 || statusCode(err) == 422 || activity.GetInfo(ctx).Attempt >= 3 {
		return failedClassification("classifier_unavailable_or_unreadable"), nil
	}
	return nil, err
}

func (a *Activities) Save(ctx context.Context, input map[string]any) (map[string]any, error) {
	if err := a.authorized(ctx); err != nil {
		return nil, err
	}
	source := object(input["source"])
	path, err := sourcePath(source, "/result")
	if err != nil {
		return nil, err
	}
	return a.Client.R2(ctx, path, map[string]any{"event_id": source["event_id"], "revision": source["revision"],
		"classification_key": source["classification_key"], "result": input["classification"]})
}

func (a *Activities) PlanQA(ctx context.Context, input map[string]any) (map[string]any, error) {
	if err := a.authorized(ctx); err != nil {
		return nil, err
	}
	source := object(input["source"])
	saved := object(input["saved"])
	path, err := sourcePath(source, "/qa-handoffs")
	if err != nil {
		return nil, err
	}
	return a.Client.R2(ctx, path, map[string]any{"event_id": source["event_id"],
		"revision": object(saved["snapshot"])["revision"], "applied": boolValue(saved, "applied")})
}

func (a *Activities) SubmitQA(ctx context.Context, input map[string]any) (map[string]any, error) {
	if err := a.authorized(ctx); err != nil {
		return nil, err
	}
	handoff := object(input["handoff"])
	id := text(handoff, "handoff_id")
	if len(id) != 64 || strings.Trim(id, "0123456789abcdef") != "" {
		return nil, temporal.NewNonRetryableApplicationError("invalid handoff identity", "LienInputInvalid", nil)
	}
	path, err := sourcePath(handoff, "/qa-handoffs/"+id)
	if err != nil {
		return nil, err
	}
	current, err := a.Client.R2(ctx, path+"/read", map[string]any{})
	if err != nil {
		return nil, err
	}
	if !boolValue(current, "current") {
		return map[string]any{"status": "superseded", "handoff_id": id}, nil
	}
	if boolValue(current, "accepted") {
		return map[string]any{"status": "accepted", "handoff_id": id, "reused": true}, nil
	}
	receipt, err := a.Client.QA(ctx, object(current["payload"]), id)
	if err == nil && (!boolValue(receipt, "accepted") || text(receipt, "handoff_id") != id) {
		err = fmt.Errorf("QA did not acknowledge the matching handoff")
	}
	if err != nil {
		return nil, err
	}
	_, err = a.Client.R2(ctx, path+"/ack", runIdentity(ctx))
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": "accepted", "handoff_id": id}, nil
}

func (a *Activities) Finish(ctx context.Context, input map[string]any) (map[string]any, error) {
	if err := a.authorized(ctx); err != nil {
		return nil, err
	}
	source := object(input["source"])
	saved := object(input["saved"])
	path, err := sourcePath(source, "/finish")
	if err != nil {
		return nil, err
	}
	status := "completed"
	if !boolValue(saved, "applied") {
		status = "superseded"
	}
	result, err := a.Client.R2(ctx, path, map[string]any{"event_id": source["event_id"], "status": status})
	if err != nil {
		return nil, err
	}
	result["status"] = status
	result["tag_status"] = saved["status"]
	result["qa_status"] = object(input["plan"])["status"]
	return result, nil
}
