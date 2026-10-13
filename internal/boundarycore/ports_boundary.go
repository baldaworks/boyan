package boundarycore

import (
	"context"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// BoundaryStore is the narrow persistence port required by boundary
// synthesis. It deliberately contains only the exact-scope stage replay,
// snapshot, and commit operations.
type BoundaryStore interface {
	LookupOperation(ctx context.Context, lookup memory.OperationLookup) (memory.OperationLookupResult, error)
	LoadBoundarySnapshot(ctx context.Context, scope memory.Scope, at time.Time) (memory.BoundarySnapshot, error)
	Commit(ctx context.Context, request memory.CommitRequest) (memory.OperationOutcome, error)
}
