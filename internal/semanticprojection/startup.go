package semanticprojection

import (
	"context"
	"errors"

	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

type initialization struct {
	done chan struct{}
	err  error
}

func closedSignal() chan struct{} {
	done := make(chan struct{})
	close(done)

	return done
}

func (manager *Manager) start(parent context.Context) error {
	manager.lifecycleMu.Lock()
	if manager.closed {
		manager.lifecycleMu.Unlock()

		return ErrClosed
	}

	if err := parent.Err(); err != nil {
		manager.lifecycleMu.Unlock()

		return err
	}

	if manager.started {
		manager.lifecycleMu.Unlock()

		return nil
	}

	if current := manager.initializing; current != nil {
		manager.lifecycleMu.Unlock()

		return manager.waitInitialization(parent, current)
	}

	current := &initialization{done: make(chan struct{})}
	manager.initializing = current
	manager.initializations.Add(1)
	drained := manager.producerIdle

	manager.lifecycleMu.Unlock()
	defer manager.initializations.Done()

	ctx, cancel := context.WithCancel(parent)

	stopShutdown := context.AfterFunc(manager.shutdown, cancel)
	defer func() { stopShutdown(); cancel() }()

	if manager.initializationElected != nil {
		manager.initializationElected()
	}

	err := manager.initialize(ctx, drained)
	manager.completeInitialization(parent, ctx, current, err)

	return manager.initializationResult(parent, current)
}

func (manager *Manager) initialize(ctx context.Context, drained <-chan struct{}) error {
	select {
	case <-drained:
	case <-ctx.Done():
		return ctx.Err()
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	var err error
	if manager.lifecycle != nil {
		// Composition grants this Manager exclusive producer ownership. New
		// admissions remain closed until this quiescent global repair finishes.
		err = manager.lifecycle.RepairSemanticOrphans(ctx)
	} else {
		// Explicit temporary legacy compatibility until runtime cutover/removal.
		err = manager.store.CollectGarbage(ctx)
	}

	if err == nil && manager.beforeStartCommit != nil {
		manager.beforeStartCommit()
	}

	return err
}

func (manager *Manager) completeInitialization(parent, ctx context.Context, current *initialization, err error) {
	manager.lifecycleMu.Lock()
	defer manager.lifecycleMu.Unlock()

	if manager.closed {
		err = ErrClosed
	} else if ctx.Err() != nil {
		err = ctx.Err()
	}

	if err == nil {
		manager.clearCleanupDeferred()
		manager.startupReason = ""
		workerContext, cancel := context.WithCancel(parent)
		manager.cancel = cancel
		manager.started = true
		manager.workers.Add(semanticcore.SemanticWorkerLimit)

		for range semanticcore.SemanticWorkerLimit {
			go manager.worker(workerContext)
		}

		go func() { <-workerContext.Done(); manager.stop() }()
	} else {
		manager.startupReason, _ = classifyFailure(err)
	}

	current.err = initializationError(err)
	manager.initializing = nil

	close(current.done)
}

func (manager *Manager) waitInitialization(ctx context.Context, current *initialization) error {
	if manager.joinedInitialization != nil {
		manager.joinedInitialization()
	}

	select {
	case <-current.done:
	case <-ctx.Done():
	}

	return manager.initializationResult(ctx, current)
}

func (manager *Manager) initializationResult(ctx context.Context, current *initialization) error {
	manager.lifecycleMu.Lock()
	closed := manager.closed
	manager.lifecycleMu.Unlock()

	if closed {
		return ErrClosed
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	return current.err
}

// The error text is fixed while Unwrap preserves cancellation and storage
// classifications. Unknown adapter prose never becomes a startup diagnostic.
type startupFailure struct{ cause error }

func (*startupFailure) Error() string     { return "semantic projection initialization failed" }
func (err *startupFailure) Unwrap() error { return err.cause }

func initializationError(err error) error {
	if err == nil {
		return nil
	}

	if _, _, classified := memory.ClassifyError(err); !classified &&
		!errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, ErrClosed) {
		err = memory.PermanentError(memory.CodeStoreFailure, "semantic projection initialization failed", err)
	}

	return &startupFailure{cause: err}
}

// lifecycleMu is held and producer admission is closed after quiescent repair.
// Clearing flags changes no state/counters and never blindly certifies ready.
func (manager *Manager) clearCleanupDeferred() {
	manager.statusMu.Lock()
	defer manager.statusMu.Unlock()

	for key, status := range manager.statuses {
		status.CleanupDeferred = false
		status.CleanupReason = ""
		manager.statuses[key] = status
	}
}

func (manager *Manager) startupStatus(status ScopeStatus, reason semanticcore.Reason) ScopeStatus {
	status.State = StateDegraded
	status.SafeReason = reason
	status.ModelSpace = manager.spaceID
	status.UpdatedAt = manager.now().UTC()

	return status
}
