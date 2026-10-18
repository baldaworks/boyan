package canonicalops

import (
	"context"
	"fmt"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

type CanonicalBoundaryStore struct {
	store  BoundaryCanonicalStore
	reader BoundaryCanonicalReader
}

// BoundaryCanonicalStore contains the existing ports consumed by the boundary
// adapter, without requiring unrelated delivery or maintenance capabilities.
type BoundaryCanonicalStore interface {
	memory.CanonicalOperationReader
	memory.CanonicalOperationCommitter
	memory.CanonicalBoundaryIdentityStore
	ApplyCanonicalMutation(ctx context.Context, mutation memory.CanonicalMutation) (memory.CanonicalMutationOutcome, error)
	LoadCanonicalRevisions(ctx context.Context,
		request memory.CanonicalRevisionReadRequest) ([]memory.MemoryRevision, error)
}

// BoundaryCanonicalReader supplies snapshot and historical replay presentation
// from the reader bound to the same canonical authority.
type BoundaryCanonicalReader interface {
	memory.CanonicalBoundaryViewReader
	LoadCanonicalCompatibility(ctx context.Context, scope memory.Scope,
		revisionID string) (memory.CanonicalCompatibilityPayload, bool, error)
}

func NewCanonicalBoundaryStore(
	store BoundaryCanonicalStore,
	reader BoundaryCanonicalReader,
) (
	*CanonicalBoundaryStore,
	error,
) {
	if store == nil || reader == nil {
		return nil, fmt.Errorf("canonical boundary store dependencies are required")
	}

	return &CanonicalBoundaryStore{store: store, reader: reader}, nil
}

// VerifyCanonicalBoundary applies exact input proof and bounded legacy policy.
func (s *CanonicalBoundaryStore) VerifyCanonicalBoundary(
	ctx context.Context, boundary memory.Boundary,
) (memory.BoundaryIdentityResult, error) {
	return s.store.VerifyCanonicalBoundary(ctx, boundary)
}
