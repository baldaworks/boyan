package memory

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
