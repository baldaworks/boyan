package postgres

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

var (
	_ memory.CanonicalTurnIdentityStore     = (*Store)(nil)
	_ memory.CanonicalBoundaryIdentityStore = (*Store)(nil)
)

// VerifyCanonicalTurn validates immutable authoritative input.
func (s *Store) VerifyCanonicalTurn(ctx context.Context, turn memory.Turn) (memory.CanonicalTurnIdentityResult, error) {
	return s.canonicalOwner().VerifyCanonicalTurn(ctx, turn)
}

// VerifyCanonicalBoundary validates input proof or new-scope capability.
func (s *Store) VerifyCanonicalBoundary(ctx context.Context,
	boundary memory.Boundary) (memory.BoundaryIdentityResult, error) {
	return s.canonicalOwner().VerifyCanonicalBoundary(ctx, boundary)
}
