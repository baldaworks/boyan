package orderedingest

import (
	"time"

	scopelanes "github.com/baldaworks/boyan/internal/scopelanes"
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
	lanes     *scopelanes.Executor

	turn struct {
		processor  TurnProcessor
		derivation memory.DerivationRef
		projection ProjectionSyncer
		repair     *projectionRepairer
	}

	boundary struct {
		processor  BoundaryProcessor
		projection ProjectionSyncer
		repair     *projectionRepairer
	}
}

var _ TurnIngestor = (*SequencedIngestRuntime)(nil)
var _ BoundaryIngestor = (*SequencedIngestRuntime)(nil)
var _ Lifecycle = (*SequencedIngestRuntime)(nil)
