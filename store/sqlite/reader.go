package sqlite

import (
	"context"
	"github.com/baldaworks/boyan/internal/canonicalsql"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

var _ memory.RecallCanonicalReader = (*Reader)(nil)
var _ memory.CanonicalScopeStatePager = (*Store)(nil)

// ScanActiveHeads reads a bounded exclusive page from this authority.
func (s *Store) ScanActiveHeads(ctx context.Context, r memory.ActiveHeadScanRequest) ([]memory.ItemHead, error) {
	return s.canonicalOwner().ScanActiveHeads(ctx, r)
}

// ScanActiveMemory reads current heads and revisions in one snapshot.
func (s *Store) ScanActiveMemory(ctx context.Context,
	r memory.ActiveMemoryScanRequest) ([]memory.ActiveCanonicalMemory, error) {
	return s.canonicalOwner().ScanActiveMemory(ctx, r)
}

// SourceRevisionBatch reads a bounded exact-source descendant page.
func (s *Store) SourceRevisionBatch(ctx context.Context, scope memory.Scope, source, after string,
	limit uint32) ([]string, string, error) {
	return s.canonicalOwner().SourceRevisionBatch(ctx, scope, source, after, limit)
}

// ListCanonicalScopeStates enumerates exact scopes with an opaque exclusive cursor.
func (s *Store) ListCanonicalScopeStates(ctx context.Context, cursor []byte, limit int) ([]memory.ScopeState,
	[]byte, error) {
	return s.canonicalOwner().ListCanonicalScopeStates(ctx, cursor, limit)
}
func (r *Reader) canonicalOwner() *canonicalsql.Owner {
	if r == nil {
		return nil
	}

	return r.store.canonicalOwner()
}

// CurrentScopeChangeSeq returns the exact scope's durable watermark.
func (r *Reader) CurrentScopeChangeSeq(ctx context.Context, scope memory.Scope) (uint64, error) {
	return r.canonicalOwner().CurrentScopeChangeSeq(ctx, scope)
}

// LoadRecallRecords hydrates only current authorized canonical candidates.
func (r *Reader) LoadRecallRecords(ctx context.Context, scope memory.Scope,
	ids []string) ([]memory.RecallRecord, error) {
	return r.canonicalOwner().LoadRecallRecords(ctx, scope, ids)
}

// SearchRecallTail returns bounded canonical fallback candidates.
func (r *Reader) SearchRecallTail(ctx context.Context, request memory.RecallRequest,
	limit uint32) ([]memory.RecallRecord, error) {
	return r.canonicalOwner().SearchRecallTail(ctx, request, limit)
}

// SearchDerived returns compatible canonical derived references.
func (r *Reader) SearchDerived(ctx context.Context,
	request memory.DerivedSearchRequest) (memory.DerivedSearchResponse, error) {
	return r.canonicalOwner().SearchDerived(ctx, request)
}
