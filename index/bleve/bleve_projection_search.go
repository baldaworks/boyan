package bleve

import (
	"context"
	"fmt"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// SearchRecall queries only the active generation and returns bounded
// metadata. Canonical hydration and fail-closed validation happen elsewhere.
func (p *BleveRecallProjection) SearchRecall(ctx context.Context, request memory.RecallRequest) ([]memory.RecallProjectionHit, error) {
	if err := canonicalContextError(ctx); err != nil {
		return nil, err
	}
	normalized, err := memory.NormalizeRecallRequest(request)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, memory.PermanentError(memory.CodeDisabled, "bleve projection is unavailable", nil)
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	index := p.activeByScope[projectionScopeKey(normalized.Scope)]
	if index == nil {
		index = p.active
	}
	if index == nil {
		return []memory.RecallProjectionHit{}, nil
	}
	searchRequest := buildRecallSearchRequest(normalized)
	result, err := index.SearchInContext(ctx, searchRequest)
	if err != nil {
		return nil, fmt.Errorf("search bleve recall projection: %w", err)
	}
	return recallProjectionHits(result.Hits)
}
