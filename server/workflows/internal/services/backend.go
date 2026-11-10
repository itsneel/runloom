package services

import (
	"context"
	"github.com/example/runloom/workflows/internal/access"
	"github.com/example/runloom/workflows/internal/budget"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/prompts"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/example/runloom/workflows/internal/usage"
)

// Backend composes local ownership with explicit service reads. All execution
// state and audit writes remain local to Workflows and its worker.
type Backend struct {
	store.Backend
	Role, Team            string
	Workflows, Management *Client
}

func (b *Backend) workflow(ctx context.Context, op string, input, out any) error {
	return b.Workflows.call(ctx, "teams/"+b.Team+"/"+op, input, out)
}
func (b *Backend) management(ctx context.Context, op string, input, out any) error {
	return b.Management.call(ctx, "teams/"+b.Team+"/"+op, input, out)
}

func (b *Backend) Get(id string, v *int) (out model.Definition, err error) {
	if b.Role == "workflows" || b.Role == "worker" {
		return b.Backend.Get(id, v)
	}
	err = b.workflow(context.Background(), "definition/get", struct {
		ID      string
		Version *int
	}{id, v}, &out)
	return
}
func (b *Backend) List() (out []store.Summary, err error) {
	if b.Role == "workflows" || b.Role == "worker" {
		return b.Backend.List()
	}
	err = b.workflow(context.Background(), "definition/list", struct{}{}, &out)
	return
}
func (b *Backend) GetRun(id string) (out store.RunRecord, err error) {
	if b.Role == "workflows" || b.Role == "worker" {
		return b.Backend.GetRun(id)
	}
	err = b.workflow(context.Background(), "run/get", struct{ ID string }{id}, &out)
	return
}
func (b *Backend) ListAudit(run, definition string, limit int) (out []store.AuditEvent, err error) {
	if b.Role == "workflows" || b.Role == "worker" {
		return b.Backend.ListAudit(run, definition, limit)
	}
	err = b.workflow(context.Background(), "audit/list", struct {
		Run, Definition string
		Limit           int
	}{run, definition, limit}, &out)
	return
}
func (b *Backend) ProjectCatalog(ctx context.Context) (store.ProjectCatalog, error) {
	var projects, policies store.ProjectCatalog
	var err error
	if b.Role == "workflows" || b.Role == "worker" {
		projects, err = b.Backend.ProjectCatalog(ctx)
		if err != nil {
			return projects, err
		}
		err = b.management(ctx, "policies/get", struct{}{}, &policies)
	} else {
		err = b.workflow(ctx, "projects/get", struct{}{}, &projects)
		if err != nil {
			return projects, err
		}
		policies, err = b.Backend.ProjectCatalog(ctx)
	}
	if err != nil {
		return projects, err
	}
	projects.BudgetPolicies = policies.BudgetPolicies
	projects.BudgetChanges = policies.BudgetChanges
	return projects, nil
}
func (b *Backend) SaveBudgetPolicy(ctx context.Context, p budget.Policy, actor string) (budget.Policy, error) {
	if b.Role != "management" {
		return p, access.ErrForbidden
	}
	return b.Backend.SaveBudgetPolicy(ctx, p, actor)
}
func (b *Backend) ListPrompts(ctx context.Context) (out []prompts.Entry, err error) {
	if b.Role == "management" {
		return b.Backend.ListPrompts(ctx)
	}
	err = b.management(ctx, "prompts/list", struct{}{}, &out)
	return
}
func (b *Backend) GetPrompt(ctx context.Context, id string, v int) (out prompts.Entry, err error) {
	if b.Role == "management" {
		return b.Backend.GetPrompt(ctx, id, v)
	}
	err = b.management(ctx, "prompts/get", struct {
		ID      string
		Version int
	}{id, v}, &out)
	return
}
func (b *Backend) PromptHistory(ctx context.Context, id string) (out []prompts.Entry, err error) {
	if b.Role == "management" {
		return b.Backend.PromptHistory(ctx, id)
	}
	err = b.management(ctx, "prompts/history", struct{ ID string }{id}, &out)
	return
}
func (b *Backend) SavePrompt(ctx context.Context, p prompts.Entry, publish bool, actor string) (prompts.Entry, error) {
	if b.Role != "management" {
		return p, access.ErrForbidden
	}
	return b.Backend.SavePrompt(ctx, p, publish, actor)
}
func (b *Backend) BeginUsage(ctx context.Context, e usage.Entry) (out bool, err error) {
	if b.Role == "management" {
		return b.Backend.BeginUsage(ctx, e)
	}
	err = b.management(ctx, "usage/begin", e, &out)
	return
}
func (b *Backend) FinishUsage(ctx context.Context, e usage.Entry) error {
	if b.Role == "management" {
		return b.Backend.FinishUsage(ctx, e)
	}
	var out bool
	return b.management(ctx, "usage/finish", e, &out)
}
func (b *Backend) QueryUsage(ctx context.Context, f usage.Filter) (out []usage.Entry, err error) {
	if b.Role == "management" {
		return b.Backend.QueryUsage(ctx, f)
	}
	err = b.management(ctx, "usage/query", f, &out)
	return
}
func (b *Backend) AggregateSpend(ctx context.Context, start, end string) (out []store.SpendTotal, err error) {
	if b.Role == "management" {
		return b.Backend.AggregateSpend(ctx, start, end)
	}
	err = b.management(ctx, "usage/aggregate", struct{ Start, End string }{start, end}, &out)
	return
}
