package postgres

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

var (
	_ memory.CanonicalOperationReader    = (*Store)(nil)
	_ memory.CanonicalOperationCommitter = (*Store)(nil)
)

// ApplyCanonicalMutation delegates to the admitted shared canonical owner.
func (s *Store) ApplyCanonicalMutation(ctx context.Context,
	mutation memory.CanonicalMutation) (memory.CanonicalMutationOutcome, error) {
	return s.canonicalOwner().ApplyCanonicalMutation(ctx, mutation)
}

// CommitCanonicalOperation delegates to the admitted shared canonical owner.
func (s *Store) CommitCanonicalOperation(ctx context.Context,
	request memory.CanonicalOperationCommitRequest) (memory.CanonicalMutationOutcome, error) {
	return s.canonicalOwner().CommitCanonicalOperation(ctx, request)
}

// LoadScopeState delegates to the admitted shared canonical owner.
func (s *Store) LoadScopeState(ctx context.Context, scope memory.Scope) (memory.ScopeState, error) {
	return s.canonicalOwner().LoadScopeState(ctx, scope)
}

// ScanScopeChanges delegates to the admitted shared canonical owner.
func (s *Store) ScanScopeChanges(ctx context.Context, scope memory.Scope, after uint64,
	limit uint32) ([]memory.ScopeChange, error) {
	return s.canonicalOwner().ScanScopeChanges(ctx, scope, after, limit)
}

// LoadCanonicalRevisions delegates to the admitted shared canonical owner.
func (s *Store) LoadCanonicalRevisions(ctx context.Context,
	request memory.CanonicalRevisionReadRequest) ([]memory.MemoryRevision, error) {
	return s.canonicalOwner().LoadCanonicalRevisions(ctx, request)
}

// LoadCanonicalOperation delegates to the admitted shared canonical owner.
func (s *Store) LoadCanonicalOperation(ctx context.Context, scope memory.Scope,
	id string) (memory.CanonicalOperationRecord, bool, error) {
	return s.canonicalOwner().LoadCanonicalOperation(ctx, scope, id)
}
