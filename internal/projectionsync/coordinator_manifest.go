package projectionsync

import (
	"context"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func (c *ProjectionCoordinator) loadOrCreateManifest(
	ctx context.Context,
	scope Scope,
	projectionID string,
	generationID string,
) (ProjectionManifest, error) {
	manifest, found, err := c.checkpoints.LoadProjectionManifest(ctx, scope, projectionID, generationID)
	if err != nil {
		return ProjectionManifest{}, err
	}
	if found && manifest.GenerationID == generationID {
		return manifest, nil
	}
	return ProjectionManifest{
		Scope:        scope,
		ProjectionID: projectionID,
		GenerationID: generationID,
		Status:       ProjectionGenerationBuilding,
		UpdatedAt:    c.currentTime().UTC(),
	}, nil
}
