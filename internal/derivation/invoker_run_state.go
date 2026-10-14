package derivation

import (
	"context"
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (i *Invoker) prepareRunState(ctx context.Context, runtime *builtRuntime) (string, error) {
	id, err := processorRunStateID(i.runStateEntropy)
	if err != nil {
		return "", runStatePreparationError(ctx)
	}
	// ADK Create atomically rejects occupied IDs. Only a successful creator may
	// install cleanup; a collision never grants authority to delete that state.
	if err := createRunState(ctx, runtime.stateStore, runtime.appName, id); err != nil {
		return "", runStatePreparationError(ctx)
	}

	return id, nil
}

func runStatePreparationError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return memory.RetryableError(memory.CodeTimeout, "prepare Boyan derivation runtime state timed out", nil)
	}

	return memory.RetryableError(memory.CodeModelFailure, "prepare Boyan derivation runtime state", nil)
}
