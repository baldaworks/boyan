package orderedingest

import (
	scopelanes "github.com/baldaworks/boyan/internal/scopelanes"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// NewSequencedIngestRuntime constructs the process-boundary ordered-ingest
// runtime over existing portable processors and a durable ingest sequence
// store.
func NewSequencedIngestRuntime(
	sequences memory.IngestSequenceStore,
	canonical memory.CanonicalStore,
	turnProcessor TurnProcessor,
	derivation memory.DerivationRef,
	turnProjection ProjectionSyncer,
	boundaryProcessor BoundaryProcessor,
	boundaryProjection ProjectionSyncer,
	config SequencedIngestConfig,
) (*SequencedIngestRuntime, error) {
	return newSequencedIngestRuntimeWithParts(
		sequences,
		canonical,
		turnProcessor,
		derivation,
		turnProjection,
		boundaryProcessor,
		boundaryProjection,
		config,
	)
}

func newSequencedIngestRuntimeWithParts(
	sequences memory.IngestSequenceStore,
	canonical memory.CanonicalStore,
	turnProcessor TurnProcessor,
	derivation memory.DerivationRef,
	turnProjection ProjectionSyncer,
	boundaryProcessor BoundaryProcessor,
	boundaryProjection ProjectionSyncer,
	config SequencedIngestConfig,
) (*SequencedIngestRuntime, error) {
	if sequences == nil || canonical == nil {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "sequenced ingest dependencies are required", nil)
	}
	if turnProcessor == nil && boundaryProcessor == nil {
		return nil, memory.PermanentError(memory.CodeDisabled, "sequenced ingest requires at least one capability", nil)
	}
	lanes, err := scopelanes.NewExecutor(config.Lanes)
	if err != nil {
		return nil, err
	}
	retryDelay := normalizeSequencedProjectionRetryDelay(config)
	runtime := &SequencedIngestRuntime{sequences: sequences, canonical: canonical, lanes: lanes}
	if err := applySequencedTurnParts(runtime, turnProcessor, derivation, turnProjection, retryDelay); err != nil {
		return nil, err
	}
	applySequencedBoundaryParts(runtime, boundaryProcessor, boundaryProjection, retryDelay)
	return runtime, nil
}
