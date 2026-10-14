package derivation

import (
	"context"
	"io"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (i *Invoker) ensureRuntime(ctx context.Context) (*builtRuntime, error) {
	for {
		runtime, build, owner, err := i.runtimeBuildStatus()
		if err != nil || runtime != nil {
			return runtime, err
		}

		if owner {
			return i.buildAndPublishRuntime(ctx, build)
		}

		if err := waitForRuntimeBuild(ctx, build); err != nil {
			return nil, err
		}
	}
}

func (i *Invoker) runtimeBuildStatus() (*builtRuntime, *runtimeBuildState, bool, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.closed {
		return nil, nil, false, runtimeClosedError()
	}

	if i.runtime != nil {
		return i.runtime, nil, false, nil
	}

	if i.building != nil {
		return nil, i.building, false, nil
	}

	i.building = newRuntimeBuildState()

	return nil, i.building, true, nil
}

func waitForRuntimeBuild(ctx context.Context, build *runtimeBuildState) error {
	select {
	case <-build.done:
		return nil
	case <-ctx.Done():
		return derivationContextError(ctx, "wait for Boyan derivation runtime")
	}
}

func (i *Invoker) buildAndPublishRuntime(
	ctx context.Context,
	build *runtimeBuildState,
) (*builtRuntime, error) {
	runtime, err := i.buildRuntime(ctx)
	if err == nil && ctx.Err() != nil {
		_ = closeBuiltRuntime(runtime)
		runtime = nil
		err = derivationContextError(ctx, "build Boyan derivation runtime")
	}

	closed := i.publishRuntimeBuild(build, runtime, err)
	if closed {
		_ = closeBuiltRuntime(runtime)

		return nil, runtimeClosedError()
	}

	return runtime, err
}

func (i *Invoker) publishRuntimeBuild(build *runtimeBuildState, runtime *builtRuntime, buildErr error) bool {
	i.mu.Lock()
	defer i.mu.Unlock()

	if !i.closed && buildErr == nil {
		i.runtime = runtime
	}

	if i.building == build {
		i.building = nil
	}

	build.finish()

	return i.closed
}

func runtimeClosedError() error {
	return memory.PermanentError(memory.CodeModelFailure, "Boyan derivation runtime is closed", nil)
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
