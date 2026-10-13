package memory

import (
	"context"
	"time"
)

// ProjectionCheckpointStore persists generation manifests independently from
// any concrete projection engine. A coordinator marks a generation Dirty
// before applying a batch, advances the watermark only after the engine commit,
// then atomically activates the clean generation.
type ProjectionCheckpointStore interface {
	LoadProjectionManifest(
		ctx context.Context,
		scope Scope,
		projectionID, generationID string,
	) (
		ProjectionManifest,
		bool,
		error,
	)
	// LoadActiveProjectionManifest may return the pointed generation as Dirty
	// during interrupted sync. Readers must require Active to certify freshness.
	LoadActiveProjectionManifest(ctx context.Context, scope Scope, projectionID string) (ProjectionManifest, bool, error)
	MarkProjectionDirty(ctx context.Context, manifest ProjectionManifest) error
	AdvanceProjectionWatermark(
		ctx context.Context,
		manifest ProjectionManifest,
		watermark uint64,
		updatedAt time.Time,
	) error
	ActivateProjectionGeneration(ctx context.Context, manifest ProjectionManifest, updatedAt time.Time) error
}

// ProjectionApplier is implemented by a disposable index or materialized-view
// adapter. Apply must be idempotent for a generation/watermark pair; Commit
// makes its preceding batch durable before the checkpoint advances.
type ProjectionApplier interface {
	Apply(
		ctx context.Context,
		scope Scope,
		generationID string,
		changes []ScopeChange,
		revisions []MemoryRevision,
	) error
	Commit(ctx context.Context, scope Scope, generationID string) error
}

// ProjectionGenerationActivator is an optional applier extension used by
// projections with an explicit disposable-generation marker (for example
// Bleve). The coordinator invokes it only after the canonical watermark has
// been durably advanced, so a committed but dirty generation is never
// advertised.
type ProjectionGenerationActivator interface {
	Activate(ctx context.Context, scope Scope, generationID string) error
}
