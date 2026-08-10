package app

import (
	"context"
	"sync"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

const defaultProjectionRepairRetryDelay = 100 * time.Millisecond

type projectionRepairer struct {
	syncer     ProjectionSyncer
	retryDelay time.Duration

	mu      sync.Mutex
	started bool
	closed  bool
	pending map[string]memory.Scope
	signal  chan struct{}
	cancel  context.CancelFunc
	done    chan struct{}
}

func newProjectionRepairer(syncer ProjectionSyncer, retryDelay time.Duration) *projectionRepairer {
	if retryDelay <= 0 {
		retryDelay = defaultProjectionRepairRetryDelay
	}
	return &projectionRepairer{
		syncer:     syncer,
		retryDelay: retryDelay,
		pending:    make(map[string]memory.Scope),
		signal:     make(chan struct{}),
	}
}

func (r *projectionRepairer) Start(parent context.Context) error {
	if r == nil {
		return nil
	}
	if parent == nil {
		parent = context.Background()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return memory.PermanentError(memory.CodeShuttingDown, "projection repairer is closed", nil)
	}
	if r.started {
		return nil
	}
	ctx, cancel := context.WithCancel(parent)
	r.cancel = cancel
	r.done = make(chan struct{})
	r.started = true
	go func(done chan struct{}) {
		defer close(done)
		r.run(ctx)
	}(r.done)
	return nil
}

func (r *projectionRepairer) Mark(scope memory.Scope) error {
	if r == nil {
		return nil
	}
	if err := scope.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.started {
		return memory.PermanentError(memory.CodeDisabled, "projection repairer is not started", nil)
	}
	if r.closed {
		return memory.PermanentError(memory.CodeShuttingDown, "projection repairer is closing", nil)
	}
	r.pending[string(scope.Kind)+"\x00"+scope.Key] = scope
	r.signalLocked()
	return nil
}

func (r *projectionRepairer) Close(ctx context.Context) error {
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
	cancel := r.cancel
	done := r.done
	r.signalLocked()
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return memory.RetryableError(memory.CodeTimeout, "projection repairer close timed out", ctx.Err())
	}
}

func (r *projectionRepairer) run(ctx context.Context) {
	for {
		scope, ok := r.next(ctx)
		if !ok {
			return
		}
		if err := r.syncer.Sync(ctx, scope); err != nil {
			if ctx.Err() != nil {
				return
			}
			timer := time.NewTimer(r.retryDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				r.requeue(scope)
				return
			case <-timer.C:
			}
			r.requeue(scope)
		}
	}
}

func (r *projectionRepairer) next(ctx context.Context) (memory.Scope, bool) {
	for {
		r.mu.Lock()
		for key, scope := range r.pending {
			delete(r.pending, key)
			r.mu.Unlock()
			return scope, true
		}
		signal := r.signal
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return memory.Scope{}, false
		case <-signal:
		}
	}
}

func (r *projectionRepairer) requeue(scope memory.Scope) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.pending[string(scope.Kind)+"\x00"+scope.Key] = scope
	r.signalLocked()
}

func (r *projectionRepairer) signalLocked() {
	close(r.signal)
	r.signal = make(chan struct{})
}
