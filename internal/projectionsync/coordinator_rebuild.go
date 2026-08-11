package projectionsync

import (
	"context"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func (c *ProjectionCoordinator) rebuildFromCanonical(ctx context.Context, scope Scope, generationID string) (uint64, error) {
	builder, err := NewScopeViewBuilder(c.canonical)
	if err != nil {
		return 0, err
	}
	if rebuilder, ok := c.applier.(ProjectionBatchRebuildApplier); ok {
		return c.rebuildProjectionBatches(ctx, scope, generationID, builder, rebuilder)
	}
	rebuilder, ok := c.applier.(ProjectionRebuildApplier)
	if !ok {
		return 0, PermanentError(CodeConflict, "projection watermark is below the retention floor and requires a rebuild", nil)
	}
	view, err := builder.Build(ctx, ProjectionViewRequest{Scope: scope, ActiveLimit: maxCanonicalReadRecords})
	if err != nil {
		return 0, err
	}
	if view.ActiveTruncated {
		return 0, PermanentError(CodeLimitExceeded, "projection rebuild requires a batch-capable applier", nil)
	}
	if err := rebuilder.Rebuild(ctx, scope, generationID, view); err != nil {
		return 0, err
	}
	return view.State.ChangeSeq, nil
}

func (c *ProjectionCoordinator) rebuildProjectionBatches(ctx context.Context, scope Scope, generationID string, builder *ScopeViewBuilder, rebuilder ProjectionBatchRebuildApplier) (uint64, error) {
	if err := rebuilder.BeginProjectionRebuild(ctx, scope, generationID); err != nil {
		return 0, err
	}
	after := ""
	var watermark uint64
	for {
		view, err := builder.Build(ctx, ProjectionViewRequest{
			Scope:             scope,
			ActiveAfterItemID: after,
			ActiveLimit:       c.batchSize,
			ActiveOnly:        true,
		})
		if err != nil {
			return 0, err
		}
		watermark = view.State.ChangeSeq
		if err := rebuilder.ApplyProjectionRebuildBatch(ctx, scope, generationID, view); err != nil {
			return 0, err
		}
		if len(view.Active) == 0 || !view.ActiveTruncated {
			break
		}
		after = view.Active[len(view.Active)-1].Item.ItemID
	}
	if err := rebuilder.EndProjectionRebuild(ctx, scope, generationID); err != nil {
		return 0, err
	}
	return watermark, nil
}
