package services

import (
	"context"

	"github.com/example/runloom/management/internal/model"

	"github.com/example/runloom/management/internal/store"
)

// Backend composes local ownership with explicit service reads. All execution
// state and audit writes remain local to Workflows and its worker.
type Backend struct {
	store.Backend
	Team      string
	Workflows *Client
}

func (b *Backend) workflow(ctx context.Context, op string, input, out any) error {
	return b.Workflows.call(ctx, "teams/"+b.Team+"/"+op, input, out)
}
func (b *Backend) Get(id string, v *int) (out model.Definition, err error) {
	err = b.workflow(context.Background(), "definition/get", struct {
		ID      string
		Version *int
	}{id, v}, &out)
	return
}
func (b *Backend) List() (out []store.Summary, err error) {
	err = b.workflow(context.Background(), "definition/list", struct{}{}, &out)
	return
}
func (b *Backend) ListAudit(run, definition string, limit int) (out []store.AuditEvent, err error) {
	err = b.workflow(context.Background(), "audit/list", struct {
		Run, Definition string
		Limit           int
	}{run, definition, limit}, &out)
	return
}
func (b *Backend) ProjectCatalog(ctx context.Context) (store.ProjectCatalog, error) {
	var p store.ProjectCatalog
	if e := b.workflow(ctx, "projects/get", struct{}{}, &p); e != nil {
		return p, e
	}
	own, e := b.Backend.ProjectCatalog(ctx)
	p.BudgetPolicies = own.BudgetPolicies
	p.BudgetChanges = own.BudgetChanges
	return p, e
}
