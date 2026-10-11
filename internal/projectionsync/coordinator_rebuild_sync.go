package projectionsync

import (
	"context"

	. "github.com/baldaworks/boyan/pkg/memory"
)

// Rebuild creates a clean disposable generation from canonical active state,
// then replays changes committed while the bounded snapshot was being scanned.
func (c *ProjectionCoordinator) Rebuild(
	ctx context.Context,
	scope Scope,
	projectionID, generationID string,
) (
	ProjectionManifest,
	error,
) {
	manifest, err := c.loadOrCreateManifest(ctx, scope, projectionID, generationID)
	if err != nil {
		return manifest, err
	}

	if err := c.checkpoints.MarkProjectionDirty(ctx, manifest); err != nil {
		return manifest, err
	}

	watermark, err := c.rebuildFromCanonical(ctx, scope, generationID)
	if err != nil {
		return manifest, err
	}

	if err := c.applier.Commit(ctx, scope, generationID); err != nil {
		return manifest, err
	}

	if err := c.checkpoints.AdvanceProjectionWatermark(ctx, manifest, watermark, c.currentTime().UTC()); err != nil {
		return manifest, err
	}

	return c.Sync(ctx, scope, projectionID, generationID)
}
