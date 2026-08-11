package scopelanes

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func prepareScopeLaneWork(
	executor *Executor,
	ctx context.Context,
	scope memory.Scope,
	run func(context.Context) error,
) (string, scopeLaneWork, error) {
	if executor == nil {
		return "", scopeLaneWork{}, memory.PermanentError(memory.CodeDisabled, "scope lane executor is unavailable", nil)
	}
	if ctx == nil {
		return "", scopeLaneWork{}, memory.PermanentError(memory.CodeInvalidDerived, "scope lane context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return "", scopeLaneWork{}, memory.RetryableError(memory.CodeTimeout, "scope lane context ended", err)
	}
	if err := scope.Validate(); err != nil {
		return "", scopeLaneWork{}, err
	}
	if run == nil {
		return "", scopeLaneWork{}, memory.PermanentError(memory.CodeInvalidDerived, "scope lane work is required", nil)
	}
	return scopeLaneKey(scope), scopeLaneWork{ctx: ctx, run: run, done: make(chan error, 1)}, nil
}

func waitForScopeLaneAvailability(ctx context.Context, changed <-chan struct{}) error {
	select {
	case <-ctx.Done():
		return memory.RetryableError(memory.CodeTimeout, "scope lane context ended", ctx.Err())
	case <-changed:
		return nil
	default:
		return memory.RetryableError(memory.CodeLimitExceeded, "scope lane queue is full", nil)
	}
}
