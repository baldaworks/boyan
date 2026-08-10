package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// LoadRecallRecords hydrates only the requested revision IDs. Missing,
// inactive, denied, or foreign candidates are skipped so a stale disposable
// projection cannot make an otherwise valid recall request fail.
func (r *CanonicalReader) LoadRecallRecords(ctx context.Context, scope memory.Scope, revisionIDs []string) ([]memory.RecallRecord, error) {
	if err := canonicalReaderContext(ctx); err != nil {
		return nil, err
	}
	if r == nil || r.store == nil {
		return nil, memory.PermanentError(memory.CodeDisabled, "canonical reader is unavailable", nil)
	}
	request := memory.CanonicalRevisionReadRequest{Scope: scope, RevisionIDs: revisionIDs}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	result := make([]memory.RecallRecord, 0, len(revisionIDs))
	for _, revisionID := range revisionIDs {
		record, found, err := r.loadRecallRecord(ctx, scope, revisionID, true)
		if err != nil {
			return nil, err
		}
		if found {
			result = append(result, record)
		}
	}
	return result, nil
}

// SearchRecallTail returns a bounded active-head page. Text matching and
// ranking remain in RecallService; this adapter never scans complete history.
func (r *CanonicalReader) SearchRecallTail(ctx context.Context, request memory.RecallRequest, limit uint32) ([]memory.RecallRecord, error) {
	if err := canonicalReaderContext(ctx); err != nil {
		return nil, err
	}
	if r == nil || r.store == nil {
		return nil, memory.PermanentError(memory.CodeDisabled, "canonical reader is unavailable", nil)
	}
	normalized, err := memory.NormalizeRecallRequest(request)
	if err != nil {
		return nil, err
	}
	if limit == 0 || limit > memory.MaxRecallCandidates {
		return nil, memory.PermanentError(memory.CodeLimitExceeded, "canonical recall tail limit is invalid", nil)
	}
	active, err := r.store.ScanActiveMemory(ctx, memory.ActiveMemoryScanRequest{Scope: normalized.Scope, Limit: limit})
	if err != nil {
		return nil, err
	}
	result := make([]memory.RecallRecord, 0, len(active))
	for _, memory := range active {
		record, found, loadErr := r.loadRecallRecord(ctx, normalized.Scope, memory.RevisionID, true)
		if loadErr != nil {
			return nil, loadErr
		}
		if found {
			result = append(result, record)
		}
	}
	return result, nil
}

func (r *CanonicalReader) loadRecallRecord(ctx context.Context, scope memory.Scope, revisionID string, activeOnly bool) (memory.RecallRecord, bool, error) {
	view, found, err := r.loadCanonicalView(ctx, scope, revisionID, activeOnly)
	if err != nil || !found {
		return memory.RecallRecord{}, found, err
	}
	text, err := r.store.LoadPayload(ctx, view.revision.Payload)
	if err != nil {
		if isCanonicalMissing(err) {
			return memory.RecallRecord{}, false, nil
		}
		return memory.RecallRecord{}, false, err
	}
	category := recallCategory(view.item.Kind)
	textValue := string(text)
	var legacyKind *memory.DerivedKind
	var topicKey, title string
	var legacyItemID, legacyRevisionID, legacyOperationID string
	var legacySupersedes *memory.RevisionRef
	var legacyParents []memory.RevisionRef
	if compatibility, ok := canonicalCompatibilityPayload(text, scope); ok {
		textValue = compatibility.Text
		kind := compatibility.Kind
		legacyKind = &kind
		legacyItemID = compatibility.LegacyItemID
		legacyRevisionID = compatibility.LegacyRevisionID
		legacyOperationID = compatibility.LegacyOperationID
		if compatibility.Supersedes != nil {
			copyOf := *compatibility.Supersedes
			legacySupersedes = &copyOf
		}
		legacyParents = append([]memory.RevisionRef(nil), compatibility.LegacyParents...)
		if compatibility.Category != nil {
			category = *compatibility.Category
		}
		topicKey = compatibility.TopicKey
		title = compatibility.Title
	}
	record := memory.RecallRecord{
		Scope:             scope,
		ItemID:            view.item.ItemID,
		RevisionID:        view.revision.RevisionID,
		Revision:          view.revision.Revision,
		Kind:              view.item.Kind,
		Category:          &category,
		MemoryKey:         view.item.MemoryKey,
		Text:              textValue,
		LegacyKind:        legacyKind,
		LegacyItemID:      legacyItemID,
		LegacyRevisionID:  legacyRevisionID,
		LegacyOperationID: legacyOperationID,
		LegacySupersedes:  legacySupersedes,
		LegacyParents:     legacyParents,
		TopicKey:          topicKey,
		Title:             title,
		State:             memory.RevisionStateActive,
		CreatedAt:         view.revision.Temporal.ObservedAt,
		Temporal:          view.revision.Temporal,
		Sensitivity:       view.revision.Sensitivity,
		Retention:         view.revision.Retention,
		Evidence:          append([]memory.EvidenceRef(nil), view.revision.Evidence...),
		ScopeChangeSeq:    0,
	}
	state, err := r.store.LoadScopeState(ctx, scope)
	if err != nil {
		return memory.RecallRecord{}, false, err
	}
	record.ScopeChangeSeq = state.ChangeSeq
	for _, source := range view.sourceRefs {
		if source.Scope != scope {
			return memory.RecallRecord{}, false, memory.PermanentError(memory.CodeScopeViolation, "canonical recall source scope does not match", nil)
		}
	}
	record.SourceIDs = make([]string, 0, len(view.sourceRefs))
	for _, source := range view.sourceRefs {
		record.SourceIDs = append(record.SourceIDs, source.ExportID)
	}
	// Canonical source IDs are the stable source identities used in
	// EvidenceRef. Keep them as the public source filter keys and retain the
	// session IDs separately for session filtering.
	record.SourceIDs = record.SourceIDs[:0]
	for _, evidence := range view.revision.Evidence {
		record.SourceIDs = appendUnique(record.SourceIDs, evidence.SourceID)
	}
	record.SourceRefs = append([]memory.SourceRef(nil), view.sourceRefs...)
	record.SessionIDs = append([]string(nil), view.sessions...)
	return record, true, nil
}
