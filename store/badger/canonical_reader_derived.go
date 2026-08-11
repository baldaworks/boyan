package badger

import (
	"context"

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
	if err := r.ensureDerivedSearchWatermark(ctx, normalized); err != nil {
		return memory.DerivedSearchResponse{}, err
	}
	// The legacy atom kind maps to both canonical state and event records;
	// category filtering below preserves the caller's requested distinction.
	records, err := r.SearchRecallTail(ctx, derivedRecallRequest(normalized), uint32(memory.MaxRecallCandidates))
	if err != nil {
		return memory.DerivedSearchResponse{}, err
	}
	results, err := appendDerivedSearchResults(records, normalized)
	if err != nil {
		return memory.DerivedSearchResponse{}, err
	}
	return derivedSearchResponse(normalized, results)
}
