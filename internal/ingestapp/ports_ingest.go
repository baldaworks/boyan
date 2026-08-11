package ingestapp

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// TurnIngestor accepts a validated terminal turn. The implementation owns
// semantic extraction, evidence grounding, temporal interpretation, and the
// idempotent canonical commit.
type TurnIngestor interface {
	IngestTurn(ctx context.Context, turn memory.Turn) error
}

// BoundaryIngestor applies a validated conversation boundary. Boundary
// synthesis is kept separate from turn ingestion so hosts can grant only the
// capability needed by a worker.
type BoundaryIngestor interface {
	ApplyBoundary(ctx context.Context, boundary memory.Boundary) error
}
