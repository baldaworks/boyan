package projectionsync

import (
	"context"
	"fmt"

	. "github.com/baldaworks/boyan/pkg/memory"
)

// Build returns active heads plus bounded recent and changed revisions. The
// request's cursors are exact canonical change sequences, so callers can
// resume a view without relying on wall-clock timestamps.
func (b *ScopeViewBuilder) Build(ctx context.Context, request ProjectionViewRequest) (BoundedProjectionView, error) {
	if b == nil || b.canonical == nil {
		return BoundedProjectionView{}, fmt.Errorf("scope view builder is required")
	}
	normalized, err := request.Normalize()
	if err != nil {
		return BoundedProjectionView{}, err
	}
	state, err := b.canonical.LoadScopeState(ctx, normalized.Scope)
	if err != nil {
		return BoundedProjectionView{}, err
	}
	if state.Scope != normalized.Scope {
		return BoundedProjectionView{}, PermanentError(CodeScopeViolation, "canonical scope state does not match the projection view", nil)
	}
	view := BoundedProjectionView{SchemaVersion: ProjectionViewSchemaVersion, State: state}
	view.Active, view.ActiveTruncated, err = b.loadActive(ctx, normalized.Scope, normalized.ActiveAfterItemID, normalized.ActiveLimit)
	if err != nil {
		return BoundedProjectionView{}, err
	}
	recentAfter := normalized.RecentAfter
	if recentAfter == 0 && state.ChangeSeq > uint64(normalized.RecentLimit) {
		recentAfter = state.ChangeSeq - uint64(normalized.RecentLimit)
	}
	view.Recent, err = b.loadChanges(ctx, normalized.Scope, recentAfter, normalized.RecentLimit)
	if err != nil {
		return BoundedProjectionView{}, err
	}
	view.Changed, err = b.loadChanges(ctx, normalized.Scope, normalized.ChangedAfter, normalized.ChangedLimit)
	if err != nil {
		return BoundedProjectionView{}, err
	}
	if err := view.Validate(); err != nil {
		return BoundedProjectionView{}, err
	}
	return view, nil
}
