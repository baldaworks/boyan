package sqlite

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

var _ memory.CanonicalForgetEnumerator = (*Store)(nil)
var _ memory.CanonicalSourceIdentityResolver = (*Store)(nil)

// ScrubCanonicalForget removes the bounded legacy selection of payloads.
func (s *Store) ScrubCanonicalForget(ctx context.Context, scope memory.Scope, sources, revisions []string) error {
	return s.canonicalOwner().ScrubCanonicalForget(ctx, scope, sources, revisions)
}

// CanonicalSourceID resolves a public reference to its canonical evidence identity.
func (s *Store) CanonicalSourceID(ctx context.Context, scope memory.Scope, ref memory.SourceRef) (string, error) {
	return s.canonicalOwner().CanonicalSourceID(ctx, scope, ref)
}

// ListCanonicalSourceRefs enumerates non-denied exact-scope source identities.
func (s *Store) ListCanonicalSourceRefs(ctx context.Context, scope memory.Scope, after string,
	limit uint32) ([]memory.SourceRef, string, error) {
	return s.canonicalOwner().ListCanonicalSourceRefs(ctx, scope, after, limit)
}

// ListCanonicalRevisionRefs enumerates non-denied exact-scope revision identities.
func (s *Store) ListCanonicalRevisionRefs(ctx context.Context, scope memory.Scope, after string,
	limit uint32) ([]memory.RevisionRef, string, error) {
	return s.canonicalOwner().ListCanonicalRevisionRefs(ctx, scope, after, limit)
}
