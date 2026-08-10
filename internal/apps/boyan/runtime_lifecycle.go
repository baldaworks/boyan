package app

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Start starts resources in canonical-to-application order. A failure closes
// every resource that was started in the current attempt.
func (r *Runtime) Start(ctx context.Context) error {
	if r == nil {
		return memory.PermanentError(memory.CodeDisabled, "boyan runtime is unavailable", nil)
	}
	if ctx == nil {
		return memory.PermanentError(memory.CodeInvalidDerived, "boyan runtime start context is required", nil)
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return memory.PermanentError(memory.CodeShuttingDown, "boyan runtime is closed", nil)
	}
	if r.started == len(r.lifecycle) {
		r.mu.Unlock()
		return nil
	}
	if r.starting {
		r.mu.Unlock()
		return memory.RetryableError(memory.CodeUnavailable, "boyan runtime is starting", nil)
	}
	r.starting = true
	startAt := r.started
	resources := append([]Lifecycle(nil), r.lifecycle...)
	r.mu.Unlock()

	for index := startAt; index < len(resources); index++ {
		if err := resources[index].Start(ctx); err != nil {
			for closeIndex := index; closeIndex >= startAt; closeIndex-- {
				_ = resources[closeIndex].Close(context.Background())
			}
			r.mu.Lock()
			r.starting = false
			r.started = startAt
			r.mu.Unlock()
			return err
		}
		r.mu.Lock()
		r.started = index + 1
		r.mu.Unlock()
	}
	r.mu.Lock()
	r.starting = false
	r.mu.Unlock()
	return nil
}

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
