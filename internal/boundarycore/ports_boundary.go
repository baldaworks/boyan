package boundarycore

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// BoundaryStore is the narrow persistence port required by boundary
// synthesis. It deliberately contains only the exact-scope stage replay,
// snapshot, and commit operations.
type BoundaryStore interface {
	LookupOperation(ctx context.Context, lookup memory.OperationLookup) (memory.OperationLookupResult, error)
	LoadScope(ctx context.Context, scope memory.Scope) (memory.ScopeSnapshot, error)
	Commit(ctx context.Context, request memory.CommitRequest) (memory.OperationOutcome, error)
}
