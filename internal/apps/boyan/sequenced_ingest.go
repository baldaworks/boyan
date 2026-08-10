package app

import (
	"context"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// SequencedIngestConfig controls the process-boundary ordered-ingest services.
type SequencedIngestConfig struct {
	Lanes                ScopeLaneConfig
	ProjectionRetryDelay time.Duration
}

// SequencedIngestRuntime provides producer/scope ordered turn and boundary
// ingestion with durable replay and projection-lag separation.
type SequencedIngestRuntime struct {
	sequences memory.IngestSequenceStore
	canonical memory.CanonicalStore
	lanes     *scopeLaneExecutor

	turn struct {
		processor  sequencedTurnCanonicalProcessor
		derivation memory.DerivationRef
		projection ProjectionSyncer
		repair     *projectionRepairer
	}

	boundary struct {
		processor  BoundaryExecutor
		projection ProjectionSyncer
		repair     *projectionRepairer
	}
}

type sequencedTurnCanonicalProcessor interface {
	ProcessTurn(ctx context.Context, turn memory.Turn, derivation memory.DerivationRef) (memory.CanonicalMutationOutcome, error)
}

// NewSequencedIngestRuntime constructs the process-boundary app service over
// existing portable processors and a durable ingest sequence store.
func NewSequencedIngestRuntime(sequences memory.IngestSequenceStore, canonical memory.CanonicalStore, turn *TurnProcessor, boundary *BoundaryService, config SequencedIngestConfig) (*SequencedIngestRuntime, error) {
	var turnProcessor sequencedTurnCanonicalProcessor
	var derivation memory.DerivationRef
	var turnProjection ProjectionSyncer
	if turn != nil {
		turnProcessor = turn.processor
		derivation = turn.derivation
		turnProjection = turn.projection
	}
	var boundaryProcessor BoundaryExecutor
	var boundaryProjection ProjectionSyncer
	if boundary != nil {
		boundaryProcessor = boundary.processor
		boundaryProjection = boundary.projection
	}
	return newSequencedIngestRuntimeWithParts(sequences, canonical, turnProcessor, derivation, turnProjection, boundaryProcessor, boundaryProjection, config)
}

func newSequencedIngestRuntimeWithParts(sequences memory.IngestSequenceStore, canonical memory.CanonicalStore, turnProcessor sequencedTurnCanonicalProcessor, derivation memory.DerivationRef, turnProjection ProjectionSyncer, boundaryProcessor BoundaryExecutor, boundaryProjection ProjectionSyncer, config SequencedIngestConfig) (*SequencedIngestRuntime, error) {
	if sequences == nil || canonical == nil {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "sequenced ingest dependencies are required", nil)
	}
	if turnProcessor == nil && boundaryProcessor == nil {
		return nil, memory.PermanentError(memory.CodeDisabled, "sequenced ingest requires at least one capability", nil)
	}
	lanes, err := newScopeLaneExecutor(config.Lanes)
	if err != nil {
		return nil, err
	}
	retryDelay := config.ProjectionRetryDelay
	if retryDelay <= 0 {
		retryDelay = defaultProjectionRepairRetryDelay
	}
	runtime := &SequencedIngestRuntime{sequences: sequences, canonical: canonical, lanes: lanes}
	if turnProcessor != nil {
		if derivation == (memory.DerivationRef{}) {
			derivation = memory.LegacyDerivationRef()
		}
		if err := derivation.Validate(); err != nil {
			return nil, err
		}
		runtime.turn.processor = turnProcessor
		runtime.turn.derivation = derivation
		runtime.turn.projection = turnProjection
		if turnProjection != nil {
			runtime.turn.repair = newProjectionRepairer(turnProjection, retryDelay)
		}
	}
	if boundaryProcessor != nil {
		runtime.boundary.processor = boundaryProcessor
		runtime.boundary.projection = boundaryProjection
		if boundaryProjection != nil {
			runtime.boundary.repair = newProjectionRepairer(boundaryProjection, retryDelay)
		}
	}
	return runtime, nil
}

var _ SequencedTurnIngestor = (*SequencedIngestRuntime)(nil)
var _ SequencedBoundaryIngestor = (*SequencedIngestRuntime)(nil)
var _ Lifecycle = (*SequencedIngestRuntime)(nil)
