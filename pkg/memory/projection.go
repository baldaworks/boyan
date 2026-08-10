package memory

import (
	"context"
	"time"
)

// ProjectionGenerationStatus controls whether a rebuild generation is safe to
// advertise. Only Active may serve indexed results; Dirty and Building must be
// rebuilt or resumed from canonical changes.
type ProjectionGenerationStatus string

const (
	ProjectionGenerationBuilding   ProjectionGenerationStatus = "building"
	ProjectionGenerationDirty      ProjectionGenerationStatus = "dirty"
	ProjectionGenerationActive     ProjectionGenerationStatus = "active"
	ProjectionGenerationSuperseded ProjectionGenerationStatus = "superseded"
)

// ProjectionManifest is mutable metadata for one projection generation. The
// canonical change log remains authoritative; Watermark is only the highest
// sequence known to have been applied to this generation.
type ProjectionManifest struct {
	Scope        Scope                      `json:"scope"`
	ProjectionID string                     `json:"projection_id"`
	GenerationID string                     `json:"generation_id"`
	Status       ProjectionGenerationStatus `json:"status"`
	Watermark    uint64                     `json:"watermark"`
	UpdatedAt    time.Time                  `json:"updated_at"`
}

// Validate verifies a projection manifest does not expose an ambiguous
// generation or claim an uninitialized state.
func (m ProjectionManifest) Validate() error {
	if err := m.Scope.Validate(); err != nil {
		return err
	}
	if !isCanonicalID(m.ProjectionID) || !isCanonicalID(m.GenerationID) || m.UpdatedAt.IsZero() {
		return invalidDerived("projection manifest identity is invalid")
	}
	switch m.Status {
	case ProjectionGenerationBuilding, ProjectionGenerationDirty, ProjectionGenerationActive, ProjectionGenerationSuperseded:
		return nil
	default:
		return invalidDerived("projection manifest status is invalid")
	}
}

// ProjectionCheckpointStore persists generation manifests independently from
// any concrete projection engine. A coordinator marks a generation Dirty
// before applying a batch, advances the watermark only after the engine commit,
// then atomically activates the clean generation.
type ProjectionCheckpointStore interface {
	LoadProjectionManifest(ctx context.Context, scope Scope, projectionID, generationID string) (ProjectionManifest, bool, error)
	LoadActiveProjectionManifest(ctx context.Context, scope Scope, projectionID string) (ProjectionManifest, bool, error)
	MarkProjectionDirty(ctx context.Context, manifest ProjectionManifest) error
	AdvanceProjectionWatermark(ctx context.Context, manifest ProjectionManifest, watermark uint64, updatedAt time.Time) error
	ActivateProjectionGeneration(ctx context.Context, manifest ProjectionManifest, updatedAt time.Time) error
}

// ProjectionApplier is implemented by a disposable index or materialized-view
// adapter. Apply must be idempotent for a generation/watermark pair; Commit
// makes its preceding batch durable before the checkpoint advances.
type ProjectionApplier interface {
	Apply(ctx context.Context, scope Scope, generationID string, changes []ScopeChange, revisions []MemoryRevision) error
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

const (
	// ProjectionViewSchemaVersion identifies the bounded canonical view passed
	// to rebuildable materialized projections.
	ProjectionViewSchemaVersion = "boyan-projection-view/v1"

	// ScopeCheckpointSchemaVersion identifies the durable per-scope checkpoint
	// record. Checkpoints are cursors, not canonical memory state.
	ScopeCheckpointSchemaVersion = "boyan-scope-checkpoint/v1"

	defaultProjectionActiveLimit  uint32 = 128
	defaultProjectionRecentLimit  uint32 = 64
	defaultProjectionChangedLimit uint32 = 64
)

// ProjectionRetentionFloorReader reports the minimum watermark that remains
// safe for incremental replay after retention removed older change records. A
// floor greater than a generation's watermark requires a complete rebuild
// from canonical active state.
// Implementations may omit this optional port when change records are retained
// indefinitely.
type ProjectionRetentionFloorReader interface {
	LoadProjectionRetentionFloor(ctx context.Context, scope Scope, projectionID string) (uint64, error)
}

// ProjectionRetentionFloorWriter is the maintenance-side port for advancing
// the replay floor after an explicitly verified retention operation.
type ProjectionRetentionFloorWriter interface {
	ProjectionRetentionFloorReader
	SetProjectionRetentionFloor(ctx context.Context, scope Scope, projectionID string, floor uint64) error
}

// ProjectionRebuildApplier is an optional extension implemented by a
// disposable projection. Rebuild must replace the generation from the bounded
// canonical view; it must not advertise the generation until Commit returns.
type ProjectionRebuildApplier interface {
	Rebuild(ctx context.Context, scope Scope, generationID string, view BoundedProjectionView) error
}

// ProjectionBatchRebuildApplier is the streaming rebuild extension for large
// scopes. Each batch is bounded by the canonical read limit; the applier must
// keep the generation unadvertised until the coordinator calls Commit.
type ProjectionBatchRebuildApplier interface {
	BeginProjectionRebuild(ctx context.Context, scope Scope, generationID string) error
	ApplyProjectionRebuildBatch(ctx context.Context, scope Scope, generationID string, view BoundedProjectionView) error
	EndProjectionRebuild(ctx context.Context, scope Scope, generationID string) error
}
