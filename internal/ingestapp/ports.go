package ingestapp

import (
	"context"

	projection "github.com/baldaworks/boyan/internal/projection"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// BoundaryExecutor is the application-owned boundary semantic port. Hosts may
// provide a processor backed by the canonical store, while the runtime still
// owns capability composition and projection ordering.
type BoundaryExecutor interface {
	ProcessBoundary(ctx context.Context, boundary memory.Boundary) (memory.BoundaryOutcome, error)
}

type ProjectionSyncer = projection.Syncer
