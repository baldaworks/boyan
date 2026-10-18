package postgres

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"time"
)

var _ memory.ScopeEraseStore = (*Store)(nil)
var _ memory.CanonicalForgetOutcomeStore = (*Store)(nil)
var _ memory.CanonicalSourceForgetStore = (*Store)(nil)

// BeginScopeErase establishes the durable baseline and ownership.
func (s *Store) BeginScopeErase(ctx context.Context, r memory.ScopeEraseRequest) (memory.ScopeEraseJournal, error) {
	return s.canonicalOwner().BeginScopeErase(ctx, r)
}

// LoadScopeErase reads validated durable progress.
func (s *Store) LoadScopeErase(ctx context.Context, scope memory.Scope, id string) (memory.ScopeEraseJournal,
	bool, error) {
	return s.canonicalOwner().LoadScopeErase(ctx, scope, id)
}

// CompleteScopeErase refuses unfinished backend progress.
func (s *Store) CompleteScopeErase(ctx context.Context, scope memory.Scope, id string) error {
	return s.canonicalOwner().CompleteScopeErase(ctx, scope, id)
}

// RunScopeErase resumes the owned backend state machine.
func (s *Store) RunScopeErase(ctx context.Context, r memory.ScopeEraseRequest) (memory.ScopeEraseJournal, error) {
	return s.canonicalOwner().RunScopeErase(ctx, r)
}

// AdvanceScopeErase commits one atomic bounded page.
func (s *Store) AdvanceScopeErase(ctx context.Context, scope memory.Scope,
	id string) (memory.ScopeEraseJournal, error) {
	return s.canonicalOwner().AdvanceScopeErase(ctx, scope, id)
}

// ListPendingScopeErases enumerates durable recovery work.
func (s *Store) ListPendingScopeErases(ctx context.Context, cursor []byte,
	limit int) ([]memory.ScopeEraseJournal, []byte, error) {
	return s.canonicalOwner().ListPendingScopeErases(ctx, cursor, limit)
}

// DenySource persists the permanent exact-scope restriction.
func (s *Store) DenySource(ctx context.Context, scope memory.Scope, id string, at time.Time) error {
	return s.canonicalOwner().DenySource(ctx, scope, id, at)
}

// DenyRevision persists the permanent exact-scope restriction.
func (s *Store) DenyRevision(ctx context.Context, scope memory.Scope, id string, at time.Time) error {
	return s.canonicalOwner().DenyRevision(ctx, scope, id, at)
}

// LoadCanonicalForgetOutcome reads the original durable result.
func (s *Store) LoadCanonicalForgetOutcome(ctx context.Context, scope memory.Scope, id string,
	kind memory.ForgetKind) (memory.ForgetOutcome, bool, error) {
	return s.canonicalOwner().LoadCanonicalForgetOutcome(ctx, scope, id, kind)
}

// CommitCanonicalForget atomically records the result and scope change.
func (s *Store) CommitCanonicalForget(ctx context.Context,
	r memory.CanonicalForgetCommitRequest) (memory.ForgetOutcome, error) {
	return s.canonicalOwner().CommitCanonicalForget(ctx, r)
}
