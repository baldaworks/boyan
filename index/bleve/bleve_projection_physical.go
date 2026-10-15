package bleve

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// LoadActivePhysicalProjectionManifest certifies the durable checkpoint
// against the generation that is actually pinned open by this adapter.
func (p *BleveRecallProjection) LoadActivePhysicalProjectionManifest(
	ctx context.Context,
	scope memory.Scope,
	projectionID string,
) (memory.ProjectionManifest, bool, error) {
	if err := canonicalContextError(ctx); err != nil {
		return memory.ProjectionManifest{}, false, err
	}

	if p == nil || scope.Validate() != nil || projectionID == "" {
		return memory.ProjectionManifest{}, false, nil
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.physicalManifestLocked(ctx, scope, projectionID)
}

//nolint:cyclop // Every independent physical certification condition fails closed.
func (p *BleveRecallProjection) physicalManifestLocked(ctx context.Context, scope memory.Scope,
	projectionID string) (memory.ProjectionManifest, bool, error) {
	if p.closed || p.checkpoints == nil || p.projectionID != projectionID {
		return memory.ProjectionManifest{}, false, nil
	}

	generationID := p.activeScopeIDs[projectionScopeKey(scope)]
	if generationID == "" {
		generationID = p.activeID
	}

	if generationID == "" {
		return memory.ProjectionManifest{}, false, nil
	}

	manifest, found, err := p.checkpoints.LoadActiveProjectionManifest(ctx, scope, projectionID)
	if err != nil || !found {
		return memory.ProjectionManifest{}, false, err
	}

	if manifest.Validate() != nil || manifest.Scope != scope || manifest.ProjectionID != projectionID ||
		manifest.GenerationID != generationID || manifest.Status != memory.ProjectionGenerationActive {
		return memory.ProjectionManifest{}, false, nil
	}

	return manifest, true, nil
}

var _ memory.PhysicalProjectionViewReader = (*BleveRecallProjection)(nil)
