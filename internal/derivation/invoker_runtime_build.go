package derivation

import (
	"context"
	"errors"

	"github.com/baldaworks/boyan/internal/structuredderive"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (i *Invoker) buildRuntime(ctx context.Context) (*builtRuntime, error) {
	req, err := dedicatedRuntimeBuildRequest(i.providerID, i.workingDir, structuredderive.DefaultDerivationInstruction)
	if err != nil {
		return nil, err
	}

	agent, err := i.factory.Build(ctx, req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, memory.RetryableError(memory.CodeTimeout, "build Boyan derivation runtime timed out", nil)
		}

		return nil, memory.RetryableError(memory.CodeModelFailure, "build Boyan derivation runtime", nil)
	}

	return &builtRuntime{
		agent:      agent,
		stateStore: newRunStateStore(),
		appName:    defaultRuntimeAppName,
	}, nil
}
