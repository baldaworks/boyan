package projection

import (
	"bytes"
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (r *ProjectionRuntime) admitStartup() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closing {
		return memory.PermanentError(memory.CodeDisabled, "projection runtime is closing", nil)
	}

	r.active.Add(1)

	return nil
}

func (r *ProjectionRuntime) syncDurableScopes(ctx context.Context) error {
	pager, ok := r.canonical.(memory.CanonicalScopeStatePager)
	if !ok {
		return ctx.Err()
	}

	var cursor []byte

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		states, next, err := pager.ListCanonicalScopeStates(ctx, cursor, defaultProjectionBatchSize)
		if err != nil {
			return err
		}

		if invalidStartupPage(states, cursor, next) {
			return memory.PermanentError(memory.CodeStoreFailure, "scope pagination did not progress within its bound", nil)
		}

		for _, state := range states {
			if err := r.Sync(ctx, state.Scope); err != nil {
				return err
			}
		}

		if len(next) == 0 {
			return ctx.Err()
		}

		cursor = next
	}
}

func invalidStartupPage(states []memory.ScopeState, cursor, next []byte) bool {
	return len(states) > defaultProjectionBatchSize || (len(next) > 0 && bytes.Equal(next, cursor))
}
