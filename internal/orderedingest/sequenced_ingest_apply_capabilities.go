package orderedingest

import (
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func applySequencedTurnParts(
	runtime *SequencedIngestRuntime,
	processor TurnProcessor,
	derivation memory.DerivationRef,
	projection ProjectionSyncer,
	retryDelay time.Duration,
) error {
	if processor == nil {
		return nil
	}
	if derivation == (memory.DerivationRef{}) {
		derivation = memory.LegacyDerivationRef()
	}
	if err := derivation.Validate(); err != nil {
		return err
	}
	runtime.turn.processor = processor
	runtime.turn.derivation = derivation
	runtime.turn.projection = projection
	if projection != nil {
		runtime.turn.repair = newProjectionRepairer(projection, retryDelay)
	}
	return nil
}

func applySequencedBoundaryParts(
	runtime *SequencedIngestRuntime,
	processor BoundaryProcessor,
	projection ProjectionSyncer,
	retryDelay time.Duration,
) {
	if processor == nil {
		return
	}
	runtime.boundary.processor = processor
	runtime.boundary.projection = projection
	if projection != nil {
		runtime.boundary.repair = newProjectionRepairer(projection, retryDelay)
	}
}
