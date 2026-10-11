package canonicalops

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (s *CanonicalBoundaryStore) LoadScope(ctx context.Context, scope memory.Scope) (memory.ScopeSnapshot, error) {
	view, err := s.reader.LoadCanonicalBoundaryView(ctx, scope)
	if err != nil {
		return memory.ScopeSnapshot{}, err
	}

	return view.Snapshot, nil
}
