package semanticprojection

import (
	"context"

	"github.com/baldaworks/boyan/pkg/memory"
)

// Election and enrollment share lifecycleMu. Keep the established flightMu ->
// lifecycleMu order; initializer drain never acquires flightMu or waits locked.
// A late same-scope join also waits for initialization before joining any flight.
func (manager *Manager) admitFlight(ctx context.Context, scope memory.Scope, scheduled bool) (*flight, bool, error) {
	for {
		manager.flightMu.Lock()
		manager.lifecycleMu.Lock()
		if manager.closed {
			manager.lifecycleMu.Unlock()
			manager.flightMu.Unlock()

			return nil, false, ErrClosed
		}

		if err := ctx.Err(); err != nil {
			manager.lifecycleMu.Unlock()
			manager.flightMu.Unlock()

			return nil, false, err
		}

		if current := manager.initializing; current != nil {
			manager.lifecycleMu.Unlock()
			manager.flightMu.Unlock()

			if err := manager.waitInitialization(ctx, current); err != nil {
				return nil, false, err
			}

			continue
		}

		if current := manager.flights[scopeKey(scope)]; current != nil {
			manager.lifecycleMu.Unlock()
			manager.flightMu.Unlock()

			return current, false, nil
		}

		if manager.producerCount == 0 {
			manager.producerIdle = make(chan struct{})
		}

		manager.producerCount++
		manager.builds.Add(1)
		current := &flight{done: make(chan struct{}), snapshotEpoch: manager.currentRequestEpoch(), ownerScheduled: scheduled}
		manager.flights[scopeKey(scope)] = current
		manager.lifecycleMu.Unlock()
		manager.flightMu.Unlock()

		return current, true, nil
	}
}

func (manager *Manager) waitFlight(ctx context.Context, current *flight, scheduled bool) flightResult {
	if manager.joinedFlight != nil {
		manager.joinedFlight()
	}

	select {
	case <-current.done:
		return flightResult{completedEpoch: current.snapshotEpoch, err: current.err,
			retryScheduled: scheduled && !current.ownerScheduled && current.callerCanceled}
	case <-ctx.Done():
		return flightResult{err: ctx.Err()}
	}
}
