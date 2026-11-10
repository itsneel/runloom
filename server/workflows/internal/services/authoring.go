package services

import (
	"context"
	"errors"

	"github.com/example/runloom/workflows/internal/access"
	"github.com/example/runloom/workflows/internal/store"
)

// Explicitly preserve the optional atomic port through the composition adapter.
// Never route authoring writes to a remote service or grant them to a worker.
func (b *Backend) authoring() (store.Authoring, error) {
	if b.Role != "workflows" {
		return nil, access.ErrForbidden
	}
	owner, ok := b.Backend.(store.Authoring)
	if !ok {
		return nil, errors.New("atomic authoring requires PostgreSQL or replica-set MongoDB")
	}
	return owner, nil
}
func (b *Backend) AuthoringReceipt(ctx context.Context, id string) (store.AuthoringReceipt, error) {
	owner, err := b.authoring()
	if err != nil {
		return store.AuthoringReceipt{}, err
	}
	return owner.AuthoringReceipt(ctx, id)
}
func (b *Backend) CommitAuthoring(ctx context.Context, batch store.AuthoringBatch) (store.AuthoringReceipt, error) {
	owner, err := b.authoring()
	if err != nil {
		return store.AuthoringReceipt{}, err
	}
	return owner.CommitAuthoring(ctx, batch)
}
