package memory

import "context"

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
