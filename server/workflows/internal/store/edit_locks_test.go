package store

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func exerciseEditLocks(t *testing.T, b Backend) {
	t.Helper()
	ctx := context.Background()
	id := "edit-" + uuid.NewString()
	token := uuid.NewString()
	status, err := b.ChangeEditLock(ctx, id, "alice", "Alice", token, "acquire")
	require.NoError(t, err)
	require.True(t, status.Locked)
	require.Equal(t, "Alice", status.OwnerName)
	_, err = b.ChangeEditLock(ctx, id, "alice", "Alice", uuid.NewString(), "acquire")
	require.ErrorIs(t, err, ErrEditLocked, "a second tab of the same user must be blocked")
	_, err = b.ChangeEditLock(ctx, id, "bob", "Bob", token, "renew")
	require.ErrorIs(t, err, ErrEditLease, "a leaked token alone does not grant ownership")
	_, err = b.BeginDefinitionWrite(ctx, id, "alice", "")
	require.ErrorIs(t, err, ErrEditLocked, "legacy clients cannot bypass the editor")
	finish, err := b.BeginDefinitionWrite(ctx, id, "alice", token)
	require.NoError(t, err)
	_, err = b.BeginDefinitionWrite(ctx, id, "alice", token)
	require.ErrorIs(t, err, ErrEditLocked, "even the owner cannot write concurrently")
	_, err = b.ChangeEditLock(ctx, id, "alice", "Alice", token, "release")
	require.ErrorIs(t, err, ErrEditLocked)
	_, err = b.ChangeEditLock(ctx, id, "alice", "Alice", token, "renew")
	require.NoError(t, err)
	other, err := b.BeginDefinitionWrite(ctx, id+"-other", "bob", "")
	require.NoError(t, err)
	require.NoError(t, other())
	require.NoError(t, finish())
	_, err = b.ChangeEditLock(ctx, id, "alice", "Alice", token, "release")
	require.NoError(t, err)
	_, err = b.BeginDefinitionWrite(ctx, id, "alice", token)
	require.ErrorIs(t, err, ErrEditLease)
	finish, err = b.BeginDefinitionWrite(ctx, id, "bob", "")
	require.NoError(t, err)
	require.NoError(t, finish())
	status, err = b.EditLock(ctx, id)
	require.NoError(t, err)
	require.False(t, status.Locked)

	var winners atomic.Int32
	var wg sync.WaitGroup
	var winningToken string
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token := uuid.NewString()
			_, err := b.ChangeEditLock(ctx, id, "alice", "Alice", token, "acquire")
			if err == nil {
				winners.Add(1)
				winningToken = token
			} else {
				require.ErrorIs(t, err, ErrEditLocked)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), winners.Load())
	_, err = b.ChangeEditLock(ctx, id, "alice", "Alice", winningToken, "release")
	require.NoError(t, err)
}

func TestEditLockContract(t *testing.T) {
	b, err := New(t.TempDir())
	require.NoError(t, err)
	exerciseEditLocks(t, b)
}

func TestEditLeaseExpiryAndWriteReservation(t *testing.T) {
	b, err := New(t.TempDir())
	require.NoError(t, err)
	ctx := context.Background()
	token := uuid.NewString()
	id := "expire-test"
	_, err = b.ChangeEditLock(ctx, id, "alice", "Alice", token, "acquire")
	require.NoError(t, err)
	finish, err := b.BeginDefinitionWrite(ctx, id, "alice", token)
	require.NoError(t, err)
	expire := func() {
		require.NoError(t, b.updateScope(ctx, "leases", func(c *ProjectCatalog) error {
			l := c.EditLocks[id]
			l.ExpiresAt = time.Now().Add(-time.Minute)
			c.EditLocks[id] = l
			return nil
		}))
	}
	expire()
	_, err = b.ChangeEditLock(ctx, id, "bob", "Bob", uuid.NewString(), "acquire")
	require.ErrorIs(t, err, ErrEditLocked)
	require.NoError(t, finish())
	_, err = b.ChangeEditLock(ctx, id, "alice", "Alice", token, "renew")
	require.ErrorIs(t, err, ErrEditLease)
	_, err = b.ChangeEditLock(ctx, id, "alice", "Alice", token, "acquire")
	require.ErrorIs(t, err, ErrEditLease)
	_, err = b.ChangeEditLock(ctx, id, "bob", "Bob", uuid.NewString(), "acquire")
	require.NoError(t, err)
	_, err = b.BeginDefinitionWrite(ctx, id, "alice", token)
	require.ErrorIs(t, err, ErrEditLease)
}
