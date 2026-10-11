package badger

import (
	"context"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (s *CanonicalStore) AdvanceProjectionWatermark(
	ctx context.Context,
	manifest memory.ProjectionManifest,
	watermark uint64,
	updatedAt time.Time,
) error {
	return s.updateProjectionManifest(ctx, manifest, updatedAt, func(stored *memory.ProjectionManifest) error {
		if stored.Status != memory.ProjectionGenerationDirty || watermark < stored.Watermark {
			return memory.PermanentError(memory.CodeConflict, "projection watermark cannot advance", nil)
		}

		stored.Watermark = watermark

		return nil
	})
}
