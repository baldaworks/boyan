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
			return <-done
		}
		if err := waitForScopeLaneAvailability(ctx, changed); err != nil {
			return err
		}
	}
}
