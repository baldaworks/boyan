package projectionsync

import (
	"context"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func (c *ProjectionCoordinator) catchUpRetentionFloor(ctx context.Context, manifest *ProjectionManifest, projectionID, generationID string) error {
	retention, ok := c.canonical.(ProjectionRetentionFloorReader)
	if !ok {
		return nil
	}
	floor, err := retention.LoadProjectionRetentionFloor(ctx, manifest.Scope, projectionID)
	if err != nil {
		return err
	}
	if floor <= manifest.Watermark {
		return nil
	}
	watermark, err := c.rebuildFromCanonical(ctx, manifest.Scope, generationID)
	if err != nil {
		return err
	}
	if watermark < floor {
		return PermanentError(CodeStoreFailure, "projection retention floor is ahead of canonical state", nil)
	}
	if err := c.applier.Commit(ctx, manifest.Scope, generationID); err != nil {
		return err
	}
	if err := c.checkpoints.AdvanceProjectionWatermark(ctx, *manifest, watermark, c.currentTime().UTC()); err != nil {
		return err
	}
	manifest.Watermark = watermark
	return nil
}
