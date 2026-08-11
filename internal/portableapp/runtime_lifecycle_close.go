package portableapp

import "context"

// Close stops resources in reverse order and is idempotent.
func (r *Runtime) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	resources := append([]Lifecycle(nil), r.lifecycle...)
	started := r.started
	r.mu.Unlock()
	var firstErr error
	if started > len(resources) {
		started = len(resources)
	}
	for index := started - 1; index >= 0; index-- {
		if err := resources[index].Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
