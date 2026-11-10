package store

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTeamPoolReusesAndBoundsLeases(t *testing.T) {
	ctx := context.Background()
	opens := 0
	p := NewTeamPool(1, func(context.Context, string) (Backend, error) { opens++; return New(t.TempDir()) })
	a, err := p.Acquire(ctx, "a")
	require.NoError(t, err)
	b, err := p.Acquire(ctx, "a")
	require.NoError(t, err)
	require.Equal(t, 1, opens)
	_, err = p.Acquire(ctx, "b")
	require.Error(t, err)
	require.Error(t, p.Close(ctx))
	require.NoError(t, a.Close(ctx))
	require.NoError(t, a.Close(ctx))
	require.NoError(t, b.Close(ctx))
	c, err := p.Acquire(ctx, "b")
	require.NoError(t, err)
	require.Equal(t, 2, opens)
	require.NoError(t, c.Close(ctx))
	require.NoError(t, p.Close(ctx))
	_, err = p.Acquire(ctx, "a")
	require.Error(t, err)
}
