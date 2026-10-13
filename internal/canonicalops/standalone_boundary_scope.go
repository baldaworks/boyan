package canonicalops

import (
	"context"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (s *CanonicalBoundaryStore) LoadScope(ctx context.Context, scope memory.Scope) (memory.ScopeSnapshot, error) {
	view, err := s.reader.LoadCanonicalBoundaryView(ctx, scope)
	if err != nil {
		return memory.ScopeSnapshot{}, err
	}

	return view.Snapshot, nil
}

// LoadBoundarySnapshot separates structural heads from eligible model context.
func (s *CanonicalBoundaryStore) LoadBoundarySnapshot(ctx context.Context, scope memory.Scope,
	at time.Time) (memory.BoundarySnapshot, error) {
	view, err := s.reader.LoadCanonicalBoundaryViewAt(ctx, scope, at)
	if err != nil {
		return memory.BoundarySnapshot{}, err
	}

	return view.Boundary, nil
}
