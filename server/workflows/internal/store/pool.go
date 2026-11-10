package store

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// TeamPool bounds worker connection pools and reuses adapters across activities.
// Close on a borrowed handle releases a lease, not the underlying connection.
type TeamPool struct {
	mu      sync.Mutex
	limit   int
	open    func(context.Context, string) (Backend, error)
	entries map[string]*poolEntry
	closed  bool
}
type poolEntry struct {
	backend Backend
	users   int
	touched time.Time
}
type borrowedStore struct {
	Backend
	release func()
	once    sync.Once
}

func (b *borrowedStore) Close(context.Context) error { b.once.Do(b.release); return nil }

func NewTeamPool(limit int, open func(context.Context, string) (Backend, error)) *TeamPool {
	if limit < 1 {
		limit = 16
	}
	return &TeamPool{limit: limit, open: open, entries: map[string]*poolEntry{}}
}

func (p *TeamPool) Acquire(ctx context.Context, team string) (Backend, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, fmt.Errorf("team store pool closed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e := p.entries[team]
	if e == nil {
		if len(p.entries) >= p.limit {
			oldest := ""
			for id, candidate := range p.entries {
				if candidate.users == 0 && (oldest == "" || candidate.touched.Before(p.entries[oldest].touched)) {
					oldest = id
				}
			}
			if oldest == "" {
				return nil, fmt.Errorf("team store pool busy; retry")
			}
			if err := p.entries[oldest].backend.Close(ctx); err != nil {
				return nil, err
			}
			delete(p.entries, oldest)
		}
		b, err := p.open(ctx, team)
		if err != nil {
			return nil, err
		}
		e = &poolEntry{backend: b}
		p.entries[team] = e
	}
	e.users++
	e.touched = time.Now()
	return &borrowedStore{Backend: e.backend, release: func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		e.users--
		e.touched = time.Now()
	}}, nil
}

// Close is called after the worker stops and all activity leases are returned.
func (p *TeamPool) Close(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, e := range p.entries {
		if e.users != 0 {
			return fmt.Errorf("team stores still in use")
		}
	}
	p.closed = true
	var first error
	for id, e := range p.entries {
		if err := e.backend.Close(ctx); err != nil && first == nil {
			first = err
		}
		delete(p.entries, id)
	}
	return first
}
