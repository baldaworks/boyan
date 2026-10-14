package scopelanes

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (e *Executor) Execute(ctx context.Context, scope memory.Scope, run func(context.Context) error) error {
	key, work, err := prepareScopeLaneWork(e, ctx, scope, run)
	if err != nil {
		return err
	}

	for {
		done, changed, queued, err := e.tryQueueScopeLaneWork(key, work)
		if err != nil {
			return err
		}

		if queued {
			return e.awaitScopeLaneWork(ctx, key, done)
		}

		if err := waitForScopeLaneAvailability(ctx, changed); err != nil {
			return err
		}
	}
}

// Cancellation can release the caller only while its work is still pending.
// Dequeue transfers ownership under the same mutex; owned callbacks may access
// caller-captured state until they signal completion.
func (e *Executor) awaitScopeLaneWork(ctx context.Context, key string, done <-chan error) error {
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		if e.removePendingScopeLaneWork(key, done) {
			return memory.RetryableError(memory.CodeTimeout, "scope lane context ended", ctx.Err())
		}

		return <-done
	}
}

func (e *Executor) removePendingScopeLaneWork(key string, done <-chan error) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	lane := e.lanes[key]
	if lane == nil {
		return false
	}

	for index, work := range lane.pending {
		if work.done != done {
			continue
		}

		copy(lane.pending[index:], lane.pending[index+1:])
		last := len(lane.pending) - 1
		lane.pending[last] = scopeLaneWork{}
		lane.pending = lane.pending[:last]

		e.signalLaneChangeLocked()

		return true
	}

	return false
}
