package badger

import (
	"context"
	"sort"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// SearchDerived maps representable canonical revisions to the additive derived
// response contract. Canonical state/event records are exposed as atom references;
// category is derived from the durable memory kind and provenance remains
// grounded in canonical evidence.
func (r *CanonicalReader) SearchDerived(ctx context.Context, request memory.DerivedSearchRequest) (memory.DerivedSearchResponse, error) {
	if err := canonicalReaderContext(ctx); err != nil {
		return memory.DerivedSearchResponse{}, err
	}
	normalized, err := memory.NormalizeDerivedSearchRequest(request)
	if err != nil {
		return memory.DerivedSearchResponse{}, err
	}
	recallRequest := memory.RecallRequest{
		Scope: normalized.Scope, Query: normalized.Query, Limit: normalized.Limit,
		AsOf: normalized.AsOf, SourceID: normalized.SourceID, SessionID: normalized.SessionID,
		MinScopeChangeSeq: normalized.MinScopeChangeSeq,
	}
	if normalized.MinScopeChangeSeq != 0 {
		current, seqErr := r.CurrentScopeChangeSeq(ctx, normalized.Scope)
		if seqErr != nil {
			return memory.DerivedSearchResponse{}, seqErr
		}
		if current < normalized.MinScopeChangeSeq {
			return memory.DerivedSearchResponse{}, memory.RetryableError(memory.CodeConflict, "canonical search watermark has not caught up", nil)
		}
	}
	// The legacy atom kind maps to both canonical state and event records;
	// category filtering below preserves the caller's requested distinction.
	records, err := r.SearchRecallTail(ctx, recallRequest, uint32(memory.MaxRecallCandidates))
	if err != nil {
		return memory.DerivedSearchResponse{}, err
	}
	results := make([]memory.DerivedReference, 0, len(records))
	for _, record := range records {
		if !strings.Contains(strings.ToLower(record.Text), strings.ToLower(normalized.Query)) {
			continue
		}
		if normalized.AsOf != nil && record.CreatedAt.After(normalized.AsOf.UTC()) {
			continue
		}
		if normalized.MemoryKey != "" && record.MemoryKey != memory.MemoryKey(normalized.MemoryKey) {
			continue
		}
		if normalized.SourceID != "" && !canonicalRecordMatchesSource(record, normalized.SourceID) {
			continue
		}
		if normalized.SessionID != "" && !containsString(record.SessionIDs, normalized.SessionID) {
			continue
		}
		if normalized.MinScopeChangeSeq != 0 && record.ScopeChangeSeq < normalized.MinScopeChangeSeq {
			return memory.DerivedSearchResponse{}, memory.RetryableError(memory.CodeConflict, "canonical search watermark has not caught up", nil)
		}
		legacyKind := memory.DerivedKindAtom
		if record.LegacyKind != nil {
			legacyKind = *record.LegacyKind
		}
		if normalized.Kind != nil && *normalized.Kind != legacyKind {
			continue
		}
		category := recallCategory(record.Kind)
		if record.Category != nil {
			category = *record.Category
		}
		if normalized.Category != nil && (record.Category == nil || *normalized.Category != category) {
			continue
		}
		provenance := memory.Provenance{ParentRevisions: append([]memory.RevisionRef(nil), record.LegacyParents...)}
		if legacyKind == memory.DerivedKindAtom || record.LegacyKind == nil {
			if len(record.SourceRefs) > 0 {
				provenance.RawSources = append(provenance.RawSources, record.SourceRefs...)
			} else {
				for _, evidence := range record.Evidence {
					provenance.RawSources = append(provenance.RawSources, memory.SourceRef{Scope: record.Scope, ExportID: evidence.SourceID, SessionID: firstSession(record.SessionIDs), SourceTurnID: evidence.SourceID})
				}
			}
		}
		if err := provenance.Validate(record.Scope); err != nil {
			continue
		}
		itemID, revisionID := record.ItemID, record.RevisionID
		if record.LegacyKind != nil {
			itemID, revisionID = record.LegacyItemID, record.LegacyRevisionID
		}
		ref := memory.DerivedReference{
			SchemaVersion: memory.DerivedSchemaVersionV1, Trust: memory.ReferenceTrustUntrusted,
			Kind: legacyKind, Scope: record.Scope, ItemID: itemID, RevisionID: revisionID,
			Revision: record.Revision, State: memory.RevisionStateActive, Category: &category,
			TopicKey: record.TopicKey, Title: record.Title, Text: record.Text, CreatedAt: record.CreatedAt, Provenance: provenance,
		}
		if legacyKind != memory.DerivedKindAtom {
			ref.Category = nil
		}
		if err := ref.Validate(); err != nil {
			continue
		}
		results = append(results, ref)
	}
	sort.SliceStable(results, func(i, j int) bool {
		if !results[i].CreatedAt.Equal(results[j].CreatedAt) {
			return results[i].CreatedAt.After(results[j].CreatedAt)
		}
		return results[i].RevisionID < results[j].RevisionID
	})
	if len(results) > normalized.Limit {
		results = results[:normalized.Limit]
	}
	response := memory.DerivedSearchResponse{SchemaVersion: memory.DerivedSchemaVersionV1, Trust: memory.ReferenceTrustUntrusted, Scope: normalized.Scope, Results: results}
	if err := response.Validate(normalized.Limit); err != nil {
		return memory.DerivedSearchResponse{}, err
	}
	return response, nil
}

func canonicalRecordMatchesSource(record memory.RecallRecord, sourceID string) bool {
	if containsString(record.SourceIDs, sourceID) {
		return true
	}
	for _, source := range record.SourceRefs {
		if source.ExportID == sourceID {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func firstSession(values []string) string {
	if len(values) == 0 {
		return "session-unknown"
	}
	return values[0]
}
