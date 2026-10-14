package derivation

import (
	"context"
	"io"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (i *Invoker) ensureRuntime(ctx context.Context) (*builtRuntime, error) {
	for {
		i.mu.Lock()
		if i.closed {
			i.mu.Unlock()

			return nil, memory.PermanentError(memory.CodeModelFailure, "Boyan derivation runtime is closed", nil)
		}

		if i.runtime != nil {
			runtime := i.runtime
			i.mu.Unlock()

			return runtime, nil
		}

		if i.building != nil {
			buildDone := i.building.done
			i.mu.Unlock()

			select {
			case <-buildDone:
				continue
			case <-ctx.Done():
				return nil, derivationContextError(ctx, "wait for Boyan derivation runtime")
			}
		}

		build := newRuntimeBuildState()
		i.building = build
		i.mu.Unlock()

		runtime, err := i.buildRuntime(ctx)
		if err == nil && ctx.Err() != nil {
			closeBuiltRuntime(runtime)
			runtime = nil
			err = derivationContextError(ctx, "build Boyan derivation runtime")
		}

		i.mu.Lock()
		closed := i.closed
		if !closed && err == nil {
			i.runtime = runtime
		}
		if i.building == build {
			i.building = nil
		}
		build.finish()
		i.mu.Unlock()

		if closed {
			closeBuiltRuntime(runtime)

			return nil, memory.PermanentError(memory.CodeModelFailure, "Boyan derivation runtime is closed", nil)
		}

		return runtime, err
	}
}

func closeBuiltRuntime(runtime *builtRuntime) error {
	if runtime == nil || runtime.agent == nil {
		return nil
	}

	if closer, ok := runtime.agent.(io.Closer); ok {
		return closer.Close()
	}

	return nil
}
