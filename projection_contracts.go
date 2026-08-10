package boyan

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
	ProjectionViewSchemaVersion = "session-memory-projection-view/v1"

	// ScopeCheckpointSchemaVersion identifies the durable per-scope checkpoint
	// record. Checkpoints are cursors, not canonical memory state.
	ScopeCheckpointSchemaVersion = "session-memory-scope-checkpoint/v1"

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

// ProjectionViewRequest controls the bounded canonical records used by a
// materialized projection rebuild or synthesis call.
type ProjectionViewRequest struct {
	Scope             Scope  `json:"scope"`
	ActiveAfterItemID string `json:"active_after_item_id,omitempty"`
	ActiveLimit       uint32 `json:"active_limit"`
	RecentAfter       uint64 `json:"recent_after"`
	RecentLimit       uint32 `json:"recent_limit"`
	ChangedAfter      uint64 `json:"changed_after"`
	ChangedLimit      uint32 `json:"changed_limit"`
	ActiveOnly        bool   `json:"active_only,omitempty"`
}

func (r ProjectionViewRequest) normalized() (ProjectionViewRequest, error) {
	if err := r.Scope.Validate(); err != nil {
		return ProjectionViewRequest{}, err
	}
	if r.ActiveAfterItemID != "" && !isCanonicalID(r.ActiveAfterItemID) {
		return ProjectionViewRequest{}, invalidDerived("projection active cursor is invalid")
	}
	out := r
	if out.ActiveLimit == 0 {
		out.ActiveLimit = defaultProjectionActiveLimit
	}
	if out.ActiveOnly {
		out.RecentLimit = 0
		out.ChangedLimit = 0
	} else if out.RecentLimit == 0 {
		out.RecentLimit = defaultProjectionRecentLimit
	}
	if !out.ActiveOnly && out.ChangedLimit == 0 {
		out.ChangedLimit = defaultProjectionChangedLimit
	}
	for _, limit := range []uint32{out.ActiveLimit, out.RecentLimit, out.ChangedLimit} {
		if limit > maxCanonicalReadRecords {
			return ProjectionViewRequest{}, limitExceeded("projection view limit exceeds the canonical read bound")
		}
	}
	return out, nil
}

// ActiveProjectionRevision joins an active item to its current canonical
// revision. It intentionally contains metadata and evidence only; payload
// content remains behind the canonical storage port.
type ActiveProjectionRevision struct {
	Item     MemoryItem     `json:"item"`
	Revision MemoryRevision `json:"revision"`
}

// BoundedProjectionView is the rebuildable projection input. Canonical state
// and revisions remain authoritative; every slice is bounded independently.
type BoundedProjectionView struct {
	SchemaVersion   string                     `json:"schema_version"`
	State           ScopeState                 `json:"state"`
	Active          []ActiveProjectionRevision `json:"active,omitempty"`
	ActiveTruncated bool                       `json:"active_truncated,omitempty"`
	Recent          []MemoryRevision           `json:"recent,omitempty"`
	Changed         []MemoryRevision           `json:"changed,omitempty"`
}

// Validate verifies exact scope ownership, canonical record shape, and the
// bounded nature of the materialized view.
func (v BoundedProjectionView) Validate() error {
	if v.SchemaVersion != ProjectionViewSchemaVersion {
		return invalidDerived("projection view schema version is invalid")
	}
	if err := v.State.Validate(); err != nil {
		return err
	}
	if len(v.Active) > maxCanonicalReadRecords || len(v.Recent) > maxCanonicalReadRecords || len(v.Changed) > maxCanonicalReadRecords {
		return limitExceeded("projection view exceeds the canonical read bound")
	}
	seenActive := make(map[string]struct{}, len(v.Active))
	for _, active := range v.Active {
		if err := active.Item.Validate(); err != nil {
			return err
		}
		if active.Item.Scope != v.State.Scope || active.Revision.ItemID != active.Item.ItemID || active.Revision.RevisionID == "" {
			return PermanentError(CodeScopeViolation, "projection active revision does not match the scope", nil)
		}
		if err := active.Revision.Validate(); err != nil {
			return err
		}
		if _, exists := seenActive[active.Item.ItemID]; exists {
			return invalidDerived("projection view contains duplicate active items")
		}
		seenActive[active.Item.ItemID] = struct{}{}
	}
	for _, revision := range append(append([]MemoryRevision(nil), v.Recent...), v.Changed...) {
		if err := revision.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// ScopeCheckpointKind identifies the lifecycle trigger that produced a
// checkpoint. All kinds use one exact scope key and are persisted separately.
type ScopeCheckpointKind string

const (
	ScopeCheckpointTurn     ScopeCheckpointKind = "turn"
	ScopeCheckpointToken    ScopeCheckpointKind = "token"
	ScopeCheckpointTime     ScopeCheckpointKind = "time"
	ScopeCheckpointBoundary ScopeCheckpointKind = "boundary"
)

func (k ScopeCheckpointKind) Validate() error {
	switch k {
	case ScopeCheckpointTurn, ScopeCheckpointToken, ScopeCheckpointTime, ScopeCheckpointBoundary:
		return nil
	default:
		return invalidDerived("unsupported scope checkpoint kind")
	}
}

// ScopeCheckpoint is the durable per-scope cursor used by synthesis and
// projection workers. It is intentionally independent from transport types.
type ScopeCheckpoint struct {
	SchemaVersion string              `json:"schema_version"`
	Scope         Scope               `json:"scope"`
	Kind          ScopeCheckpointKind `json:"kind"`
	CheckpointID  string              `json:"checkpoint_id"`
	ScopeVersion  uint64              `json:"scope_version"`
	ChangeSeq     uint64              `json:"change_seq"`
	TokenCount    uint64              `json:"token_count,omitempty"`
	OccurredAt    time.Time           `json:"occurred_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
}

func (c ScopeCheckpoint) Validate() error {
	if c.SchemaVersion != ScopeCheckpointSchemaVersion {
		return invalidDerived("scope checkpoint schema version is invalid")
	}
	if err := c.Scope.Validate(); err != nil {
		return err
	}
	if err := c.Kind.Validate(); err != nil {
		return err
	}
	if !isCanonicalID(c.CheckpointID) || c.OccurredAt.IsZero() || c.UpdatedAt.IsZero() {
		return invalidDerived("scope checkpoint identity or timestamp is invalid")
	}
	if c.Kind != ScopeCheckpointToken && c.TokenCount != 0 {
		return invalidDerived("non-token checkpoint contains a token count")
	}
	return nil
}

// ScopeCheckpointStore persists the latest checkpoint for each exact scope
// and trigger kind. Implementations must make a checkpoint update durable
// before returning and reject backwards cursors.
type ScopeCheckpointStore interface {
	LoadScopeCheckpoint(ctx context.Context, scope Scope, kind ScopeCheckpointKind) (ScopeCheckpoint, bool, error)
	SaveScopeCheckpoint(ctx context.Context, checkpoint ScopeCheckpoint) error
}
