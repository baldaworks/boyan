package projectionsync

import (
	"context"
	"fmt"

	. "github.com/baldaworks/boyan/pkg/memory"
)

// Sync replays all currently available changes for one scope/generation. A
// failed apply or commit leaves the manifest Dirty, so callers fail closed and
// later retry or rebuild from canonical data.
func (c *ProjectionCoordinator) Sync(ctx context.Context, scope Scope, projectionID, generationID string) (ProjectionManifest, error) {
	if c == nil {
		return ProjectionManifest{}, fmt.Errorf("projection coordinator is required")
	}
	if err := scope.Validate(); err != nil {
		return ProjectionManifest{}, PermanentError(CodePermanent, "projection sync scope is invalid", err)
	}
	if !isCanonicalID(projectionID) || !isCanonicalID(generationID) {
		return ProjectionManifest{}, PermanentError(CodePermanent, "projection sync identity is invalid", nil)
	}
	manifest, err := c.loadOrCreateManifest(ctx, scope, projectionID, generationID)
	if err != nil {
		return ProjectionManifest{}, err
	}
	if err := c.checkpoints.MarkProjectionDirty(ctx, manifest); err != nil {
		return ProjectionManifest{}, err
	}
	manifest.Status = ProjectionGenerationDirty
	if err := c.catchUpRetentionFloor(ctx, &manifest, projectionID, generationID); err != nil {
		return manifest, err
	}
	applied, err := c.applyScopeChanges(ctx, scope, generationID, &manifest)
	if err != nil {
		return manifest, err
	}
	if !applied {
		if err := c.applier.Commit(ctx, scope, generationID); err != nil {
			return manifest, err
		}
	}
	if err := c.checkpoints.ActivateProjectionGeneration(ctx, manifest, c.currentTime().UTC()); err != nil {
		return manifest, err
	}
	if activator, ok := c.applier.(ProjectionGenerationActivator); ok {
		if err := activator.Activate(ctx, scope, generationID); err != nil {
			// The durable checkpoint is already active. The previous physical
			// generation remains safe to serve until the adapter completes its
			// marker switch; a retry can reconcile the disposable projection.
			return manifest, err
		}
	}
	manifest.Status = ProjectionGenerationActive
	return manifest, nil
}
