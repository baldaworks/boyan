package app

import (
	"context"
	"fmt"

	. "github.com/baldaworks/boyan"
)

// ScopeViewBuilder reads canonical state in bounded pages. It never loads a
// full scope and is suitable for Scenario/Profile or retrieval projection
// rebuilds.
type ScopeViewBuilder struct {
	canonical CanonicalStore
}

// NewScopeViewBuilder constructs a storage-neutral bounded-view builder.
func NewScopeViewBuilder(canonical CanonicalStore) (*ScopeViewBuilder, error) {
	if canonical == nil {
		return nil, fmt.Errorf("canonical store is required")
	}
	return &ScopeViewBuilder{canonical: canonical}, nil
}

// Build returns active heads plus bounded recent and changed revisions. The
// request's cursors are exact canonical change sequences, so callers can
// resume a view without relying on wall-clock timestamps.
func (b *ScopeViewBuilder) Build(ctx context.Context, request ProjectionViewRequest) (BoundedProjectionView, error) {
	if b == nil || b.canonical == nil {
		return BoundedProjectionView{}, fmt.Errorf("scope view builder is required")
	}
	normalized, err := normalizeProjectionViewRequest(request)
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

func (b *ScopeViewBuilder) loadActive(ctx context.Context, scope Scope, after string, limit uint32) ([]ActiveProjectionRevision, bool, error) {
	active := make([]ActiveProjectionRevision, 0, limit)
	for uint32(len(active)) < limit {
		pageLimit := limit - uint32(len(active))
		if pageLimit > maxCanonicalReadRecords {
			pageLimit = maxCanonicalReadRecords
		}
		page, err := b.canonical.ScanActiveMemory(ctx, ActiveMemoryScanRequest{Scope: scope, AfterItemID: after, Limit: pageLimit})
		if err != nil {
			return nil, false, err
		}
		if len(page) == 0 {
			break
		}
		ids := make([]string, 0, len(page))
		for _, item := range page {
			if item.Item.Scope != scope || item.RevisionID == "" {
				return nil, false, PermanentError(CodeScopeViolation, "canonical active memory has the wrong scope", nil)
			}
			ids = append(ids, item.RevisionID)
		}
		revisions, err := b.loadRevisionIDs(ctx, scope, ids)
		if err != nil {
			return nil, false, err
		}
		byID := make(map[string]MemoryRevision, len(revisions))
		for _, revision := range revisions {
			byID[revision.RevisionID] = revision
		}
		for _, item := range page {
			revision, ok := byID[item.RevisionID]
			if !ok || revision.ItemID != item.Item.ItemID {
				return nil, false, PermanentError(CodeStoreFailure, "canonical active revision is missing", nil)
			}
			active = append(active, ActiveProjectionRevision{Item: item.Item, Revision: revision})
		}
		after = page[len(page)-1].Item.ItemID
		if uint32(len(page)) < pageLimit {
			break
		}
	}
	if len(active) == 0 || uint32(len(active)) < limit {
		return active, false, nil
	}
	probe, err := b.canonical.ScanActiveMemory(ctx, ActiveMemoryScanRequest{Scope: scope, AfterItemID: active[len(active)-1].Item.ItemID, Limit: 1})
	if err != nil {
		return nil, false, err
	}
	return active, len(probe) != 0, nil
}

func (b *ScopeViewBuilder) loadChanges(ctx context.Context, scope Scope, after uint64, limit uint32) ([]MemoryRevision, error) {
	if limit == 0 {
		return nil, nil
	}
	changes, err := b.canonical.ScanScopeChanges(ctx, scope, after, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(changes))
	seen := make(map[string]struct{}, len(changes))
	for _, change := range changes {
		for _, id := range change.RevisionIDs {
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	return b.loadRevisionIDs(ctx, scope, ids)
}

func (b *ScopeViewBuilder) loadRevisionIDs(ctx context.Context, scope Scope, ids []string) ([]MemoryRevision, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	revisions := make([]MemoryRevision, 0, len(ids))
	for start := 0; start < len(ids); start += maxCanonicalReadRecords {
		end := start + maxCanonicalReadRecords
		if end > len(ids) {
			end = len(ids)
		}
		batch, err := b.canonical.LoadCanonicalRevisions(ctx, CanonicalRevisionReadRequest{Scope: scope, RevisionIDs: ids[start:end]})
		if err != nil {
			return nil, err
		}
		revisions = append(revisions, batch...)
	}
	return revisions, nil
}

// ScopeCheckpointRecorder is the lane-owned adapter used by turn, token,
// timer, and boundary triggers. It makes replay of the same cursor idempotent
// and rejects out-of-order writes before they reach durable storage.
type ScopeCheckpointRecorder struct {
	store ScopeCheckpointStore
}

func NewScopeCheckpointRecorder(store ScopeCheckpointStore) (*ScopeCheckpointRecorder, error) {
	if store == nil {
		return nil, fmt.Errorf("scope checkpoint store is required")
	}
	return &ScopeCheckpointRecorder{store: store}, nil
}

func (r *ScopeCheckpointRecorder) Record(ctx context.Context, checkpoint ScopeCheckpoint) error {
	if r == nil || r.store == nil {
		return fmt.Errorf("scope checkpoint recorder is required")
	}
	if err := checkpoint.Validate(); err != nil {
		return err
	}
	prior, found, err := r.store.LoadScopeCheckpoint(ctx, checkpoint.Scope, checkpoint.Kind)
	if err != nil {
		return err
	}
	if found {
		if err := prior.Validate(); err != nil {
			return err
		}
		if prior.Scope != checkpoint.Scope || prior.Kind != checkpoint.Kind {
			return PermanentError(CodeScopeViolation, "scope checkpoint ownership changed", nil)
		}
		if checkpoint.ScopeVersion < prior.ScopeVersion || checkpoint.ChangeSeq < prior.ChangeSeq || checkpoint.OccurredAt.Before(prior.OccurredAt) {
			return PermanentError(CodeConflict, "scope checkpoint moved backwards", nil)
		}
		if checkpoint.ScopeVersion == prior.ScopeVersion && checkpoint.ChangeSeq == prior.ChangeSeq && checkpoint.CheckpointID == prior.CheckpointID {
			return nil
		}
	}
	return r.store.SaveScopeCheckpoint(ctx, checkpoint)
}
