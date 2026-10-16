package projectionsync

import (
	"context"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func (c *ProjectionCoordinator) applyScopeChanges(
	ctx context.Context,
	scope Scope,
	generationID string,
	manifest *ProjectionManifest,
) (
	bool,
	error,
) {
	applied := false

	for {
		changes, err := c.canonical.ScanScopeChanges(ctx, scope, manifest.Watermark, c.batchSize)
		if err != nil {
			return false, err
		}

		if len(changes) == 0 {
			return applied, nil
		}

		if err := checkProjectionRebuildRequired(changes); err != nil {
			return false, err
		}

		revisions, err := c.loadRevisions(ctx, scope, uniqueRevisionIDs(changes))
		if err != nil {
			return false, err
		}

		if err := c.applier.Apply(ctx, scope, generationID, changes, revisions); err != nil {
			return false, err
		}

		watermark := changes[len(changes)-1].Sequence

		if err := c.commitProjection(ctx, *manifest, watermark); err != nil {
			return false, err
		}

		if err := c.checkpoints.AdvanceProjectionWatermark(ctx, *manifest, watermark, c.currentTime().UTC()); err != nil {
			return false, err
		}

		manifest.Watermark = watermark
		applied = true
	}
}
