package projection

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Start reconciles retained indexes and synchronously repairs durable scopes.
// Stores without optional scope pagination retain lazy per-scope synchronization.
func (r *ProjectionRuntime) Start(ctx context.Context) error {
	if r == nil || r.coordinator == nil {
		return memory.PermanentError(memory.CodeDisabled, "projection runtime is unavailable", nil)
	}

	if ctx == nil {
		return memory.PermanentError(memory.CodeInvalidDerived, "projection runtime context is required", nil)
	}

	if err := r.admitStartup(); err != nil {
		return err
	}
	defer r.active.Done()

	if err := ctx.Err(); err != nil {
		return err
	}

	if r.maintenance != nil {
		if err := r.maintenance.ReconcileProjection(ctx); err != nil {
			return err
		}
	}

	return r.syncDurableScopes(ctx)
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
