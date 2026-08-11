package orderedingest

import (
	"context"

	projection "github.com/baldaworks/boyan/internal/projection"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// TurnIngestor accepts one producer-ordered terminal turn and returns the
// durable ordered-ingest outcome.
type TurnIngestor interface {
	IngestSequencedTurn(ctx context.Context, request memory.SequencedTurn) (memory.IngestOutcome, error)
}

// BoundaryIngestor accepts one producer-ordered boundary and returns the
// durable ordered-ingest outcome.
type BoundaryIngestor interface {
	ApplySequencedBoundary(ctx context.Context, request memory.SequencedBoundary) (memory.IngestOutcome, error)
}

// TurnProcessor owns the canonical ordered-ingest commit step before projection
// lag handling is applied.
type TurnProcessor interface {
	ProcessTurn(ctx context.Context, turn memory.Turn, derivation memory.DerivationRef) (memory.CanonicalMutationOutcome, error)
}

// BoundaryProcessor applies the exact-scope boundary semantics before
// projection lag handling is applied.
type BoundaryProcessor interface {
	ProcessBoundary(ctx context.Context, boundary memory.Boundary) (memory.BoundaryOutcome, error)
}

type ProjectionSyncer = projection.Syncer

// Lifecycle starts and closes process-boundary ordered-ingest workers.
type Lifecycle interface {
	Start(ctx context.Context) error
	Close(ctx context.Context) error
}
