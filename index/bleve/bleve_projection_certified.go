package bleve

import (
	"context"
	"fmt"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// SearchRecallCertified samples the actual physical view and searches it under
// one read lock. Legacy searches retain their existing candidate policy.
//
//nolint:cyclop // Independent certification and storage failures must remain explicit under one physical lock.
func (p *BleveRecallProjection) SearchRecallCertified(ctx context.Context,
	request memory.RecallRequest, depth int) (memory.CertifiedRecallResult, error) {
	if err := canonicalContextError(ctx); err != nil {
		return memory.CertifiedRecallResult{}, err
	}

	normalized, err := memory.NormalizeRecallRequest(request)
	if err != nil {
		return memory.CertifiedRecallResult{}, err
	}

	if depth < 1 || depth > 100 {
		return memory.CertifiedRecallResult{},
			memory.PermanentError(memory.CodeInvalidQuery, "invalid recall channel depth", nil)
	}

	if p == nil {
		return memory.CertifiedRecallResult{},
			memory.PermanentError(memory.CodeDisabled, "bleve projection is unavailable", nil)
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return memory.CertifiedRecallResult{}, projectionClosedError()
	}

	before, certified, err := p.physicalManifestLocked(ctx, normalized.Scope, p.projectionID)
	if err != nil {
		return memory.CertifiedRecallResult{}, err
	}

	if normalized.MinScopeChangeSeq != 0 && (!certified || before.Watermark < normalized.MinScopeChangeSeq) {
		return memory.CertifiedRecallResult{}, recallConsistencyConflict()
	}

	hits, err := p.searchCertifiedLocked(ctx, normalized, depth)
	if err != nil {
		return memory.CertifiedRecallResult{}, err
	}

	after, stillCertified, err := p.physicalManifestLocked(ctx, normalized.Scope, p.projectionID)
	if err != nil {
		return memory.CertifiedRecallResult{}, err
	}

	certified = certified && stillCertified && before == after
	if normalized.MinScopeChangeSeq != 0 && !certified {
		return memory.CertifiedRecallResult{}, recallConsistencyConflict()
	}

	return memory.CertifiedRecallResult{Hits: hits, Manifest: before, Certified: certified}, nil
}

func (p *BleveRecallProjection) searchCertifiedLocked(ctx context.Context, request memory.RecallRequest,
	depth int) ([]memory.RecallProjectionHit, error) {
	index := p.activeByScope[projectionScopeKey(request.Scope)]
	if index == nil {
		index = p.active
	}

	if index == nil {
		return []memory.RecallProjectionHit{}, nil
	}

	query := buildRecallSearchRequest(request)
	query.Size = depth
	query.SortBy([]string{"-_score", "_id"})

	result, searchErr := index.SearchInContext(ctx, query)
	if searchErr != nil {
		return nil, fmt.Errorf("search bleve recall projection: %w", searchErr)
	}

	return recallProjectionHits(result.Hits)
}
