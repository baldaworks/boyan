package app

import (
	"context"
	"sync"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

const (
	defaultMaxConcurrentScopeLanes = 4
	defaultMaxQueuedPerScopeLane   = 32
	maxConcurrentScopeLanes        = 128
	maxQueuedPerScopeLane          = 1024
)

// ScopeLaneConfig controls exact-scope FIFO execution with bounded cross-scope
// concurrency.
type ScopeLaneConfig struct {
	MaxConcurrentScopes int
	MaxQueuedPerScope   int
}

func (c ScopeLaneConfig) normalized() (ScopeLaneConfig, error) {
	out := c
	if out.MaxConcurrentScopes <= 0 {
		out.MaxConcurrentScopes = defaultMaxConcurrentScopeLanes
	}
	if out.MaxQueuedPerScope <= 0 {
		out.MaxQueuedPerScope = defaultMaxQueuedPerScopeLane
	}
	if out.MaxConcurrentScopes > maxConcurrentScopeLanes || out.MaxQueuedPerScope > maxQueuedPerScopeLane {
		return ScopeLaneConfig{}, memory.PermanentError(memory.CodeLimitExceeded, "scope lane limits exceed safe bounds", nil)
	}
	return out, nil
}

type scopeLaneExecutor struct {
	config ScopeLaneConfig

	mu          sync.Mutex
	started     bool
	closed      bool
	lanes       map[string]*scopeLane
	laneChanged chan struct{}
	semaphore   chan struct{}
	laneWG      sync.WaitGroup
}

type scopeLane struct {
	pending []scopeLaneWork
	running bool
}

type scopeLaneWork struct {
	ctx  context.Context
	run  func(context.Context) error
	done chan error
}

func newScopeLaneExecutor(config ScopeLaneConfig) (*scopeLaneExecutor, error) {
	normalized, err := config.normalized()
	if err != nil {
		return nil, err
	}
	return &scopeLaneExecutor{
		config:      normalized,
		lanes:       make(map[string]*scopeLane),
		laneChanged: make(chan struct{}),
		semaphore:   make(chan struct{}, normalized.MaxConcurrentScopes),
	}, nil
}

func (e *scopeLaneExecutor) Start(context.Context) error {
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

func (e *scopeLaneExecutor) Execute(ctx context.Context, scope memory.Scope, run func(context.Context) error) error {
	if e == nil {
		return memory.PermanentError(memory.CodeDisabled, "scope lane executor is unavailable", nil)
	}
	if ctx == nil {
		return memory.PermanentError(memory.CodeInvalidDerived, "scope lane context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return memory.RetryableError(memory.CodeTimeout, "scope lane context ended", err)
	}
	if err := scope.Validate(); err != nil {
		return err
	}
	if run == nil {
		return memory.PermanentError(memory.CodeInvalidDerived, "scope lane work is required", nil)
	}
	key := string(scope.Kind) + "\x00" + scope.Key
	work := scopeLaneWork{ctx: ctx, run: run, done: make(chan error, 1)}
	for {
		e.mu.Lock()
		if !e.started {
			e.mu.Unlock()
			return memory.PermanentError(memory.CodeDisabled, "scope lane executor is not started", nil)
		}
		if e.closed {
			e.mu.Unlock()
			return memory.PermanentError(memory.CodeShuttingDown, "scope lane executor is closing", nil)
		}
		lane := e.lanes[key]
		if lane == nil {
			lane = &scopeLane{}
			e.lanes[key] = lane
		}
		if len(lane.pending) < e.config.MaxQueuedPerScope {
			lane.pending = append(lane.pending, work)
			if !lane.running {
				lane.running = true
				e.laneWG.Add(1)
				go e.runLane(key, lane)
			}
			e.signalLaneChangeLocked()
			e.mu.Unlock()
			return <-work.done
		}
		changed := e.laneChanged
		e.mu.Unlock()
		select {
		case <-ctx.Done():
			return memory.RetryableError(memory.CodeTimeout, "scope lane context ended", ctx.Err())
		case <-changed:
		default:
			return memory.RetryableError(memory.CodeLimitExceeded, "scope lane queue is full", nil)
		}
	}
}

func (e *scopeLaneExecutor) Close(ctx context.Context) error {
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

func (e *scopeLaneExecutor) runLane(key string, lane *scopeLane) {
	defer e.laneWG.Done()
	for {
		e.mu.Lock()
		if len(lane.pending) == 0 {
			lane.running = false
			if e.lanes[key] == lane {
				delete(e.lanes, key)
			}
			e.signalLaneChangeLocked()
			e.mu.Unlock()
			return
		}
		work := lane.pending[0]
		lane.pending[0] = scopeLaneWork{}
		lane.pending = lane.pending[1:]
		e.signalLaneChangeLocked()
		e.mu.Unlock()

		select {
		case e.semaphore <- struct{}{}:
			err := work.run(work.ctx)
			<-e.semaphore
			work.done <- err
			close(work.done)
		case <-work.ctx.Done():
			work.done <- memory.RetryableError(memory.CodeTimeout, "scope lane context ended", work.ctx.Err())
			close(work.done)
		}
	}
}

func (e *scopeLaneExecutor) signalLaneChangeLocked() {
	close(e.laneChanged)
	e.laneChanged = make(chan struct{})
}

var _ Lifecycle = (*scopeLaneExecutor)(nil)
