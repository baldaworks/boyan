package scopelanes

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (e *Executor) Start(context.Context) error {
	if e == nil {
		return memory.PermanentError(memory.CodeDisabled, "scope lane executor is unavailable", nil)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return memory.PermanentError(memory.CodeShuttingDown, "scope lane executor is closed", nil)
	}
	if e.started {
		return nil
	}
	e.started = true
	return nil
}

func (e *Executor) Close(ctx context.Context) error {
	if e == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	e.signalLaneChangeLocked()
	e.mu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		e.laneWG.Wait()
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return memory.RetryableError(memory.CodeTimeout, "scope lane executor close timed out", ctx.Err())
	}
}
