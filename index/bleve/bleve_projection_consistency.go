package bleve

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// SearchRecallConsistent pins the physical generation and verifies an unchanged,
// clean authoritative checkpoint around the query. Hydrated record sequences
// cannot establish that missing matches were indexed.
func (p *BleveRecallProjection) SearchRecallConsistent(ctx context.Context,
	request memory.RecallRequest) ([]memory.RecallProjectionHit,
	error) {
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

	if p.closed {
		return nil, projectionClosedError()
	}

	before, err := p.recallCheckpoint(ctx, normalized)
	if err != nil {
		return nil, err
	}

	hits, err := p.searchRecallLocked(ctx, normalized)
	if err != nil {
		return nil, err
	}

	after, err := p.recallCheckpoint(ctx, normalized)
	if err != nil {
		return nil, err
	}

	if before != after {
		return nil, recallConsistencyConflict()
	}

	return hits, nil
}

func (p *BleveRecallProjection) recallCheckpoint(ctx context.Context,
	request memory.RecallRequest) (memory.ProjectionManifest,
	error) {
	if p.checkpoints == nil || p.projectionID == "" {
		return memory.ProjectionManifest{}, recallConsistencyConflict()
	}

	generationID := p.activeScopeIDs[projectionScopeKey(request.Scope)]
	if generationID == "" {
		generationID = p.activeID
	}

	if generationID == "" {
		return memory.ProjectionManifest{}, recallConsistencyConflict()
	}

	manifest, found, err := p.checkpoints.LoadActiveProjectionManifest(ctx, request.Scope, p.projectionID)
	if err != nil {
		return memory.ProjectionManifest{}, err
	}

	if !found {
		return memory.ProjectionManifest{}, recallConsistencyConflict()
	}

	if err := validateRecallCheckpoint(manifest,
		request,
		p.projectionID,
		generationID); err != nil {
		return memory.ProjectionManifest{},
			err
	}

	return manifest, nil
}

func recallConsistencyConflict() error {
	return memory.RetryableError(memory.CodeConflict,
		"recall projection consistency checkpoint is unavailable or changed",
		nil)
}

var _ memory.ConsistentRecallProjection = (*BleveRecallProjection)(nil)

func validateRecallCheckpoint(manifest memory.ProjectionManifest,
	request memory.RecallRequest,
	projectionID,
	generationID string) error {
	if err := manifest.Validate(); err != nil {
		return err
	}

	if manifest.Scope != request.Scope ||
		manifest.ProjectionID != projectionID ||
		manifest.GenerationID != generationID {
		return recallConsistencyConflict()
	}

	if manifest.Status != memory.ProjectionGenerationActive || manifest.Watermark < request.MinScopeChangeSeq {
		return recallConsistencyConflict()
	}

	return nil
}
