package portableapp

import (
	"context"

	projection "github.com/baldaworks/boyan/internal/projection"
	"github.com/baldaworks/boyan/internal/structuredderive"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

const ownedLifecycleCount = 2 // Projection and invoker.

func buildRuntimeLifecycle(
	config RuntimeConfig,
	projectionRuntime *projection.ProjectionRuntime,
	invokerLifecycle *structuredInvokerLifecycle,
	project ProjectionSyncer,
) ([]Lifecycle, error) {
	lifecycle := make([]Lifecycle, 0, len(config.Lifecycle)+ownedLifecycleCount)
	// Projection is the first application-side resource: canonical commits
	// must be available before any generation can be advertised.
	if projectionRuntime != nil {
		lifecycle = append(lifecycle, projectionRuntime)
	}

	if invokerLifecycle != nil {
		lifecycle = append(lifecycle, invokerLifecycle)
	}

	if projectionRuntime == nil {
		if resource, ok := project.(Lifecycle); ok {
			lifecycle = append(lifecycle, resource)
		}
	}

	for _, resource := range config.Lifecycle {
		if resource == nil {
			return nil, memory.PermanentError(memory.CodeInvalidDerived, "runtime lifecycle dependency is nil", nil)
		}

		lifecycle = append(lifecycle, resource)
	}

	return lifecycle, nil
}

type structuredInvokerLifecycle struct {
	invoker structuredderive.StructuredInvoker
}

func (*structuredInvokerLifecycle) Start(context.Context) error { return nil }

func (l *structuredInvokerLifecycle) Close(ctx context.Context) error {
	if l == nil || l.invoker == nil {
		return nil
	}

	return l.invoker.Close(ctx)
}
