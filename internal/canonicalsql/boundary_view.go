package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"time"
)

var _ memory.CanonicalBoundaryViewReader = (*Owner)(nil)
var _ memory.CanonicalDerivedReader = (*Owner)(nil)

// LoadCanonicalBoundaryView returns bounded active compatible presentation.
func (o *Owner) LoadCanonicalBoundaryView(ctx context.Context,
	scope memory.Scope) (memory.CanonicalBoundaryView, error) {
	return o.loadCanonicalBoundaryView(ctx, scope, nil)
}

// LoadCanonicalBoundaryViewAt retains structural heads and separately marks
// factual context eligible at the caller's single processing instant.
func (o *Owner) LoadCanonicalBoundaryViewAt(ctx context.Context, scope memory.Scope,
	at time.Time) (memory.CanonicalBoundaryView, error) {
	if at.IsZero() {
		return memory.CanonicalBoundaryView{}, memory.PermanentError(memory.CodeInvalidDerived,
			"boundary processing time is required", nil)
	}

	return o.loadCanonicalBoundaryView(ctx, scope, &at)
}

func (o *Owner) loadCanonicalBoundaryView(ctx context.Context, scope memory.Scope,
	at *time.Time) (memory.CanonicalBoundaryView, error) {
	instant := time.Now()
	if at != nil {
		instant = *at
	}

	records, captured, err := o.loadBoundedBoundaryRecords(ctx, scope, memory.MaxSnapshotItems, instant)
	if err != nil {
		return memory.CanonicalBoundaryView{}, err
	}

	view, err := buildCanonicalBoundaryView(ctx, scope, captured.state.Version, records)
	if err != nil {
		return memory.CanonicalBoundaryView{}, err
	}

	if at != nil {
		view, err = temporalCanonicalBoundaryView(view, records, *at)
		if err != nil {
			return memory.CanonicalBoundaryView{}, err
		}
	}

	if err := o.finalReaderBarrier(ctx, captured); err != nil {
		return memory.CanonicalBoundaryView{}, err
	}

	return view, nil
}

// LoadActiveRecallRecords fails if the complete active view exceeds limit.
func (o *Owner) LoadActiveRecallRecords(ctx context.Context, scope memory.Scope,
	limit uint32) ([]memory.RecallRecord, error) {
	if err := readerContext(ctx, o); err != nil {
		return nil, err
	}

	if err := scope.Validate(); err != nil {
		return nil, err
	}

	if limit == 0 || limit > memory.MaxRecallCandidates {
		return nil, limitError()
	}

	records, captured, err := o.loadBoundedBoundaryRecords(ctx, scope, int(limit), time.Now())
	if err != nil {
		return nil, err
	}

	if err := o.finalReaderBarrier(ctx, captured); err != nil {
		return nil, err
	}

	return records, nil
}

func (o *Owner) loadBoundedBoundaryRecords(ctx context.Context, scope memory.Scope, limit int,
	at time.Time) ([]memory.RecallRecord, readerSnapshot, error) {
	var (
		records  []memory.RecallRecord
		captured readerSnapshot
	)
	if err := readerContext(ctx, o); err != nil {
		return nil, captured, err
	}

	if err := scope.Validate(); err != nil {
		return nil, captured, err
	}

	err := o.View(ctx, func(ctx context.Context, tx *Tx) error {
		ids, err := collectBoundaryRevisionIDs(ctx, tx, scope, limit)
		if err != nil {
			return err
		}

		records, err = loadReaderRecords(ctx, tx, scope, ids, at, &captured)

		return err
	})

	return records, captured, err
}

func collectBoundaryRevisionIDs(ctx context.Context, tx *Tx, scope memory.Scope, limit int) ([]string, error) {
	ids := make([]string, 0, limit)
	cursor := ""

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		heads, err := scanReaderHeads(ctx, tx, memory.ActiveHeadScanRequest{Scope: scope,
			AfterItemID: cursor, Limit: uint32(min(memory.MaxCanonicalReadRecords, limit-len(ids)+1))})
		if err != nil {
			return nil, err
		}

		if len(heads) == 0 {
			return ids, nil
		}

		if len(ids)+len(heads) > limit {
			return nil, limitError()
		}

		for _, head := range heads {
			if head.ItemID <= cursor {
				return nil, corruptRow()
			}

			if err := validateBoundaryHead(ctx, tx, scope, head); err != nil {
				return nil, err
			}

			cursor = head.ItemID
			ids = append(ids, head.RevisionID)
		}
	}
}

func validateBoundaryHead(ctx context.Context, tx *Tx, scope memory.Scope, head memory.ItemHead) error {
	revision, err := loadProofRevision(ctx, tx, scope, head.RevisionID)
	if err != nil {
		return err
	}

	if revision.ItemID != head.ItemID || revision.RevisionID != head.RevisionID {
		return corruptRow()
	}

	_, err = loadProofItem(ctx, tx, scope, head.ItemID)

	return err
}

// LoadCanonicalCompatibility reads an exact revision, including superseded
// revisions used by idempotent boundary replay.
func (o *Owner) LoadCanonicalCompatibility(ctx context.Context, scope memory.Scope,
	id string) (memory.CanonicalCompatibilityPayload, bool, error) {
	if err := readerContext(ctx, o); err != nil {
		return memory.CanonicalCompatibilityPayload{}, false, err
	}

	if err := (memory.CanonicalRevisionReadRequest{Scope: scope, RevisionIDs: []string{id}}).Validate(); err != nil {
		return memory.CanonicalCompatibilityPayload{}, false, err
	}

	var (
		result memory.CanonicalCompatibilityPayload
		found  bool
	)

	graph := traceGraph{captured: readerSnapshot{sources: make(map[string]memory.CanonicalSourceRecord),
		revisions: make(map[string]memory.MemoryRevision)}}

	err := o.View(ctx, func(ctx context.Context, tx *Tx) error {
		var err error

		result, found, err = loadCompatibilitySnapshot(ctx, tx, scope, id, &graph)

		return err
	})
	if err != nil {
		return memory.CanonicalCompatibilityPayload{}, false, err
	}

	if found {
		if err := o.finalTracePrivacyBarrier(ctx, graph); err != nil {
			return memory.CanonicalCompatibilityPayload{}, false, err
		}
	}

	return result, found, nil
}

func captureCompatibilitySources(ctx context.Context, tx *Tx, scope memory.Scope, view traceView,
	graph *traceGraph) error {
	for _, evidence := range view.revision.Evidence {
		source, ok, err := readerSource(ctx, tx, scope, evidence.SourceID)
		if err != nil {
			return err
		}

		if !ok {
			return readerDenied()
		}

		graph.captured.sources[source.SourceID] = source
	}

	if err := validateReaderSnapshotEvidence(ctx, tx, graph.captured); err != nil {
		return err
	}

	return nil
}

func loadCompatibilitySnapshot(ctx context.Context, tx *Tx, scope memory.Scope, id string,
	graph *traceGraph) (memory.CanonicalCompatibilityPayload, bool, error) {
	state, err := readerScopeState(ctx, tx, scope)
	if err != nil {
		return memory.CanonicalCompatibilityPayload{}, false, err
	}

	graph.captured.state = state

	view, err := loadTraceViewCore(ctx, tx, scope, id)
	if privacyMissing(err) {
		return memory.CanonicalCompatibilityPayload{}, false, nil
	}

	if err != nil {
		return memory.CanonicalCompatibilityPayload{}, false, err
	}

	graph.nodes = []traceNode{{view: view}}

	graph.captured.revisions[id] = view.revision
	if err := captureCompatibilitySources(ctx, tx, scope, view, graph); err != nil {
		return memory.CanonicalCompatibilityPayload{}, false, err
	}

	payload, err := readerPayload(ctx, tx, view.revision.Payload)
	if err != nil {
		return memory.CanonicalCompatibilityPayload{}, false, err
	}

	result, found := canonicalCompatibilityPayload(payload, scope)

	return result, found, nil
}
