package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"time"
)

var _ memory.RecallCanonicalReader = (*Owner)(nil)

type canonicalRecallView struct {
	item          memory.MemoryItem
	revision      memory.MemoryRevision
	state         memory.RevisionState
	sourceRefs    []memory.SourceRef
	sourceIDs     []string
	conversations []string
}

// CurrentScopeChangeSeq returns the exact scope's durable unsigned watermark.
func (o *Owner) CurrentScopeChangeSeq(ctx context.Context, scope memory.Scope) (uint64, error) {
	if err := readerContext(ctx, o); err != nil {
		return 0, err
	}

	state, err := o.LoadScopeState(ctx, scope)

	return state.ChangeSeq, err
}

// LoadRecallRecords hydrates only requested active revisions. Missing or denied
// stale projection candidates are omitted. Authority, payloads and watermark
// share one admitted snapshot, followed by a fresh final authority barrier.
func (o *Owner) LoadRecallRecords(ctx context.Context, scope memory.Scope,
	ids []string) ([]memory.RecallRecord, error) {
	if err := readerContext(ctx, o); err != nil {
		return nil, err
	}

	if err := (memory.CanonicalRevisionReadRequest{Scope: scope, RevisionIDs: ids}).Validate(); err != nil {
		return nil, err
	}

	var (
		records  []memory.RecallRecord
		captured readerSnapshot
	)

	err := o.View(ctx, func(ctx context.Context, tx *Tx) error {
		var err error

		records, err = loadReaderRecords(ctx, tx, scope, ids, time.Now(), &captured)

		return err
	})
	if err != nil {
		return nil, err
	}

	if err := o.finalReaderBarrier(ctx, captured); err != nil {
		return nil, err
	}

	return records, nil
}

// SearchRecallTail supplies a bounded active page; matching and ranking remain
// in RecallService. No search/projection callback runs under owner admission.
func (o *Owner) SearchRecallTail(ctx context.Context, request memory.RecallRequest,
	limit uint32) ([]memory.RecallRecord, error) {
	if err := readerContext(ctx, o); err != nil {
		return nil, err
	}

	normalized, err := memory.NormalizeRecallRequest(request)
	if err != nil {
		return nil, err
	}

	if limit == 0 || limit > memory.MaxRecallCandidates {
		return nil, limitError()
	}

	var (
		records  []memory.RecallRecord
		captured readerSnapshot
	)

	err = o.View(ctx, func(ctx context.Context, tx *Tx) error {
		var err error

		records, err = readerRecallTail(ctx, tx, normalized, limit, &captured)

		return err
	})
	if err != nil {
		return nil, err
	}

	if err := o.finalReaderBarrier(ctx, captured); err != nil {
		return nil, err
	}

	return records, nil
}

func readerRecallTail(ctx context.Context, tx *Tx, request memory.RecallRequest, limit uint32,
	captured *readerSnapshot) ([]memory.RecallRecord, error) {
	heads, err := scanReaderHeads(ctx, tx, memory.ActiveHeadScanRequest{Scope: request.Scope, Limit: limit})
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(heads))
	for _, head := range heads {
		ids = append(ids, head.RevisionID)
	}

	return loadReaderRecords(ctx, tx, request.Scope, ids, time.Now(), captured)
}

func loadReaderRecords(ctx context.Context, tx *Tx, scope memory.Scope, ids []string, at time.Time,
	captured *readerSnapshot) ([]memory.RecallRecord, error) {
	state, err := readerScopeState(ctx, tx, scope)
	if err != nil {
		return nil, err
	}

	captured.state = state

	incomplete, err := privacyScopeIncomplete(ctx, tx, scope)
	if err != nil {
		return nil, err
	}

	records := make([]memory.RecallRecord, 0, len(ids))
	if incomplete {
		return records, nil
	}

	resolver := newBoundaryBoundsResolver(ctx, tx, scope, at)
	for _, id := range ids {
		record, found, err := loadReaderRecord(ctx, tx, resolver, id)
		if err != nil {
			return nil, err
		}

		if found {
			record.ScopeChangeSeq = state.ChangeSeq
			records = append(records, record)
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	captured.revisions = resolver.revisions
	captured.sources = resolver.sources
	captured.records = records

	return records, nil
}

func readerContext(ctx context.Context, o *Owner) error {
	if err := mutationContext(ctx); err != nil {
		return err
	}

	if o == nil {
		return memory.PermanentError(memory.CodeDisabled, "canonical reader is unavailable", nil)
	}

	return nil
}

func readerScopeState(ctx context.Context, tx *Tx, scope memory.Scope) (memory.ScopeState, error) {
	if err := checkCanonicalWriterFence(ctx, tx, scope); err != nil {
		return memory.ScopeState{}, err
	}

	if err := checkScopeErasePayloadGuard(ctx, tx); err != nil {
		return memory.ScopeState{}, err
	}

	key, err := proofScopeKey(scope)
	if err != nil {
		return memory.ScopeState{}, err
	}

	return loadProofScopeState(ctx, tx, scope, key)
}
