package app

import (
	"context"
	"sync"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Runtime is the concrete portable Boyan application.  Consumers
// should depend on one or more of its capability interfaces, not on a type
// assertion for optional methods.
type Runtime struct {
	turn     *TurnProcessor
	boundary *BoundaryService
	recall   RecallReader
	trace    TraceReader
	forget   Forgetter

	mu        sync.Mutex
	lifecycle []Lifecycle
	started   int
	starting  bool
	closed    bool
}

// NewRuntime composes the supported application capabilities.  Missing
// optional capabilities leave their methods disabled; this is useful for a
// host that grants only ingestion or only read access.  Turn ingestion is
// constructed when CanonicalStore and Extractor (or StructuredInvoker) are
// present.  Recall and projection adapters are constructed from their narrow
// ports without importing a storage implementation.
func NewRuntime(config RuntimeConfig) (*Runtime, error) {
	derivation := config.Derivation
	if derivation == (memory.DerivationRef{}) {
		derivation = memory.LegacyDerivationRef()
	}
	if err := derivation.Validate(); err != nil {
		return nil, err
	}

	project := config.Project
	var projectionRuntime *ProjectionRuntime
	if project == nil && config.CanonicalStore != nil && config.ProjectionCheckpoints != nil && config.ProjectionApplier != nil && config.ProjectionID != "" {
		var err error
		projectionRuntime, err = NewProjectionRuntime(config.CanonicalStore, config.ProjectionCheckpoints, config.ProjectionApplier, config.ProjectionID, config.ProjectionBatchSize)
		if err != nil {
			return nil, err
		}
		project = projectionRuntime
	}

	extractor := config.Extractor
	var invokerLifecycle *structuredInvokerLifecycle
	if config.StructuredInvoker != nil {
		invokerLifecycle = &structuredInvokerLifecycle{invoker: config.StructuredInvoker}
	}
	if extractor == nil && config.StructuredInvoker != nil {
		deriver, err := NewDeriver(config.StructuredInvoker)
		if err != nil {
			return nil, err
		}
		extractor = deriver
	}

	var turn *TurnProcessor
	if config.CanonicalStore != nil && extractor != nil {
		var err error
		turn, err = NewTurnProcessor(config.CanonicalStore, extractor, config.Policy, derivation, project)
		if err != nil {
			return nil, err
		}
	}

	recall := config.Recall
	if recall == nil && config.RecallCanonical != nil {
		service, err := NewRecallService(config.RecallCanonical, config.RecallProjection)
		if err != nil {
			return nil, err
		}
		recall = service
	}

	trace := config.Trace
	if trace == nil && config.CanonicalDerived != nil {
		service, err := NewTraceService(config.CanonicalDerived)
		if err != nil {
			return nil, err
		}
		trace = service
	}

	var boundary *BoundaryService
	if config.Boundary != nil {
		var err error
		boundary, err = NewBoundaryService(config.Boundary, project)
		if err != nil {
			return nil, err
		}
	}

	forget := config.Forget
	if forget != nil {
		service, err := NewForgetServiceWithProjection(forget, project)
		if err != nil {
			return nil, err
		}
		forget = service
	}

	lifecycle := make([]Lifecycle, 0, len(config.Lifecycle)+2)
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
	return &Runtime{turn: turn, boundary: boundary, recall: recall, trace: trace, forget: forget, lifecycle: lifecycle}, nil
}

type structuredInvokerLifecycle struct {
	invoker StructuredInvoker
}

func (*structuredInvokerLifecycle) Start(context.Context) error { return nil }

func (l *structuredInvokerLifecycle) Close(ctx context.Context) error {
	if l == nil || l.invoker == nil {
		return nil
	}
	return l.invoker.Close(ctx)
}

var _ TurnIngestor = (*Runtime)(nil)
var _ BoundaryIngestor = (*Runtime)(nil)
var _ RecallReader = (*Runtime)(nil)
var _ TraceReader = (*Runtime)(nil)
var _ Forgetter = (*Runtime)(nil)
var _ Lifecycle = (*Runtime)(nil)
