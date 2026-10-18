package canonicalsql

import (
	"context"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// AdvanceProjectionWatermark advances only a dirty generation without regressions.
func (o *Owner) AdvanceProjectionWatermark(ctx context.Context, manifest memory.ProjectionManifest,
	watermark uint64, updatedAt time.Time) error {
	if err := validateProjectionUpdate(ctx, manifest, updatedAt); err != nil {
		return err
	}

	return o.Update(ctx, func(ctx context.Context, tx *Tx) error {
		stored, key, err := projectionForUpdate(ctx, tx, manifest)
		if err != nil {
			return err
		}

		if stored.Status != memory.ProjectionGenerationDirty || watermark < stored.Watermark {
			return memory.PermanentError(memory.CodeConflict, "projection watermark cannot advance", nil)
		}

		stored.Watermark, stored.UpdatedAt = watermark, updatedAt.UTC()

		return putPrivacyRecord(ctx, tx, key, ProjectionManifest, stored)
	})
}

func validateProjectionUpdate(ctx context.Context, manifest memory.ProjectionManifest, updatedAt time.Time) error {
	if err := mutationContext(ctx); err != nil {
		return err
	}

	if err := manifest.Validate(); err != nil || updatedAt.IsZero() {
		return memory.PermanentError(memory.CodePermanent, "projection manifest update is invalid", err)
	}

	return nil
}

func projectionForUpdate(
	ctx context.Context,
	tx *Tx,
	manifest memory.ProjectionManifest,
) (memory.ProjectionManifest, Key, error) {
	key, err := projectionManifestKey(manifest.Scope, manifest.ProjectionID, manifest.GenerationID)
	if err != nil {
		return memory.ProjectionManifest{}, key, err
	}

	var stored memory.ProjectionManifest
	if err := getPrivacyRecord(ctx, tx, key, ProjectionManifest, &stored); err != nil {
		return stored, key, err
	}

	if stored.Scope != manifest.Scope || stored.ProjectionID != manifest.ProjectionID ||
		stored.GenerationID != manifest.GenerationID {
		return stored, key, memory.PermanentError(memory.CodeConflict, "projection generation changed", nil)
	}

	return stored, key, nil
}

// ActivateProjectionGeneration publishes a dirty generation and retires its
// predecessor atomically, including their row stamps and the active pointer.
func (o *Owner) ActivateProjectionGeneration(
	ctx context.Context,
	manifest memory.ProjectionManifest,
	updatedAt time.Time,
) error {
	if err := validateProjectionUpdate(ctx, manifest, updatedAt); err != nil {
		return err
	}

	return o.Update(ctx, func(ctx context.Context, tx *Tx) error {
		stored, key, err := projectionForUpdate(ctx, tx, manifest)
		if err != nil {
			return err
		}

		if stored.Status != memory.ProjectionGenerationDirty {
			return memory.PermanentError(memory.CodeConflict, "projection generation is not dirty", nil)
		}

		activeKey, err := privacyKey(manifest.Scope, ProjectionActive, manifest.ProjectionID)
		if err != nil {
			return err
		}

		if err := retirePreviousProjection(ctx, tx, manifest, activeKey, updatedAt); err != nil {
			return err
		}

		stored.Status, stored.UpdatedAt = memory.ProjectionGenerationActive, updatedAt.UTC()
		if err := putPrivacyRecord(ctx, tx, key, ProjectionManifest, stored); err != nil {
			return err
		}

		return putPrivacyRecord(
			ctx,
			tx,
			activeKey,
			ProjectionActive,
			projectionActive{GenerationID: stored.GenerationID},
		)
	})
}

func retirePreviousProjection(
	ctx context.Context,
	tx *Tx,
	manifest memory.ProjectionManifest,
	activeKey Key,
	updatedAt time.Time,
) error {
	var active projectionActive
	if err := getPrivacyRecord(ctx, tx, activeKey, ProjectionActive, &active); privacyMissing(err) {
		return nil
	} else if err != nil {
		return err
	}

	if active.GenerationID == manifest.GenerationID {
		return nil
	}

	key, err := projectionManifestKey(manifest.Scope, manifest.ProjectionID, active.GenerationID)
	if err != nil {
		return err
	}

	var previous memory.ProjectionManifest
	if err := getPrivacyRecord(ctx, tx, key, ProjectionManifest, &previous); err != nil {
		return err
	}

	previous.Status, previous.UpdatedAt = memory.ProjectionGenerationSuperseded, updatedAt.UTC()

	return putPrivacyRecord(ctx, tx, key, ProjectionManifest, previous)
}
