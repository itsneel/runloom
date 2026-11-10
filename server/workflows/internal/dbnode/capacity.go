package dbnode

import "context"

// Bound outbound pools across all tenants in one API/worker process. Each call
// owns one SQL connection or a Mongo client with a one-connection-per-server
// operation pool (plus driver monitoring sockets); acquisition shares its deadline.
var slots = make(chan struct{}, 8)

func acquire(ctx context.Context) bool {
	select {
	case slots <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}
func release() { <-slots }
