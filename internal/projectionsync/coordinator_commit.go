package projectionsync

import (
	"context"

	. "github.com/baldaworks/boyan/pkg/memory"
)

// projectionManifestCommitter seals the coordinator's exact replay target.
// Adapters without this capability keep the legacy commit contract.
type projectionManifestCommitter interface {
	CommitProjectionManifest(ctx context.Context, manifest ProjectionManifest) error
}

func (c *ProjectionCoordinator) commitProjection(
	ctx context.Context, manifest ProjectionManifest, watermark uint64,
) error {
	if committer, ok := c.applier.(projectionManifestCommitter); ok {
		manifest.Watermark = watermark

		return committer.CommitProjectionManifest(ctx, manifest)
	}

	return c.applier.Commit(ctx, manifest.Scope, manifest.GenerationID)
}
