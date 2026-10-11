package projection

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Start satisfies Lifecycle. Projection generations are started lazily per
// scope, after the canonical runtime is available.
func (r *ProjectionRuntime) Start(ctx context.Context) error {
	if r.maintenance != nil {
		return r.maintenance.ReconcileProjection(ctx)
	}

	return nil
}

// Close prevents new syncs and waits for in-flight projection work.
func (r *ProjectionRuntime) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}

	r.mu.Lock()

	r.closing = true
	if r.drained == nil {
		r.drained = make(chan struct{})
		go func() { r.active.Wait(); close(r.drained) }()
	}

	drained := r.drained
	r.mu.Unlock()

	select {
	case <-drained:
		return nil
	case <-ctx.Done():
		return memory.RetryableError(memory.CodeTimeout, "projection drain deadline exceeded", ctx.Err())
	}
}
