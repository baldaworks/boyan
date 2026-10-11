package derivation

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (i *Invoker) ensureRuntime(ctx context.Context) (*builtRuntime, error) {
	i.mu.Lock()
	if i.closed {
		i.mu.Unlock()

		return nil, memory.PermanentError(memory.CodeModelFailure, "Boyan derivation runtime is closed", nil)
	}

	if i.runtime == nil {
		runtime, err := i.buildRuntime(ctx)
		if err != nil {
			i.mu.Unlock()

			return nil, err
		}

		i.runtime = runtime
	}

	runtime := i.runtime
	i.mu.Unlock()

	return runtime, nil
}
