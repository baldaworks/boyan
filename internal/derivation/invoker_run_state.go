package derivation

import (
	"context"
	"errors"

	"github.com/baldaworks/boyan/internal/structuredderive"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (i *Invoker) prepareRunState(
	ctx context.Context,
	runtime *builtRuntime,
	request structuredderive.StructuredInvocation,
) (string, error) {
	runStateID := processorRunStateID(request.OperationID, request.Stage)
	deleteRunState(ctx, runtime.stateStore, runtime.appName, runStateID)

	if err := createRunState(ctx, runtime.stateStore, runtime.appName, runStateID); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", memory.RetryableError(memory.CodeTimeout, "create Boyan derivation runtime state timed out", nil)
		}

		return "", memory.RetryableError(memory.CodeModelFailure, "create Boyan derivation runtime state", nil)
	}

	return runStateID, nil
}
