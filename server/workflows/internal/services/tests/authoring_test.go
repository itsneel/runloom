package tests

import (
	"context"
	"github.com/example/runloom/workflows/internal/access"
	"github.com/example/runloom/workflows/internal/services"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAuthoringPortOwnedByWorkflowAPIOnly(t *testing.T) {
	for _, role := range []string{"worker", "management", "auth", ""} {
		b := &services.Backend{Role: role}
		_, err := b.AuthoringReceipt(context.Background(), "id")
		require.ErrorIs(t, err, access.ErrForbidden)
		_, err = b.CommitAuthoring(context.Background(), store.AuthoringBatch{})
		require.ErrorIs(t, err, access.ErrForbidden)
	}
}
