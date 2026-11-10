package tenancy

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/example/runloom/workflows/internal/protection"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

// Workers hosts independent SDK pollers per org. Process resources are shared;
// this is namespace/task-queue isolation, not process or credential isolation.
type Workers struct {
	Pool        *Pool
	Build       func(client.Client) worker.Worker
	lastSuccess atomic.Int64
}

func (f *Workers) Ready() bool {
	last := f.lastSuccess.Load()
	return last > 0 && time.Since(time.Unix(0, last)) < time.Minute
}

func (f *Workers) Run(ctx context.Context) {
	type entry struct {
		client client.Client
		worker worker.Worker
	}
	active := map[string]entry{}
	defer func() {
		for _, e := range active {
			e.worker.Stop()
			e.client.Close()
		}
	}()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		check, cancel := context.WithTimeout(ctx, 20*time.Second)
		names, err := f.Pool.List(check)
		if err == nil {
			for _, name := range names {
				if _, ok := active[name]; ok {
					continue
				}
				options, e := protection.TemporalOptions(f.Pool.address, name)
				if e != nil {
					err = e
					break
				}
				c, e := client.NewClientFromExisting(f.Pool.Cluster, options)
				if e != nil {
					err = e
					break
				}
				w := f.Build(c)
				if e = w.Start(); e != nil {
					c.Close()
					err = e
					break
				}
				active[name] = entry{c, w}
				log.Printf("worker polling organization namespace %s", name)
			}
		}
		cancel()
		if err == nil {
			f.lastSuccess.Store(time.Now().UnixNano())
		} else if ctx.Err() == nil {
			log.Print("organization worker reconciliation failed; will retry (details withheld)")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
