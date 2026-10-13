package canonicalops

import (
	"context"
	"fmt"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/badger"
)

type CanonicalBoundaryStore struct {
	store  *badger.CanonicalStore
	reader *badger.CanonicalReader
}

func NewCanonicalBoundaryStore(
	store *badger.CanonicalStore,
	reader *badger.CanonicalReader,
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
