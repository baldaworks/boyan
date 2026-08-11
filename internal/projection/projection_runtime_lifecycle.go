package projection

import (
	"context"
)

// Start satisfies Lifecycle. Projection generations are started lazily per
// scope, after the canonical runtime is available.
func (*ProjectionRuntime) Start(context.Context) error { return nil }

// Close prevents new syncs and waits for in-flight projection work.
func (r *ProjectionRuntime) Close(context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	r.closing = true
	r.mu.Unlock()
	r.active.Wait()
	return nil
}
