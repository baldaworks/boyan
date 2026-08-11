package projectionsync

import (
	"context"
	"errors"

	. "github.com/baldaworks/boyan/pkg/memory"
)

type retentionProjectionStore struct {
	boundedProjectionStore
	floor uint64
}

func (s *retentionProjectionStore) LoadProjectionRetentionFloor(context.Context, Scope, string) (uint64, error) {
	return s.floor, nil
}

type retentionProjectionApplier struct {
	projectionApplier
	rebuilt bool
	view    BoundedProjectionView
}

func (a *retentionProjectionApplier) Rebuild(_ context.Context, _ Scope, _ string, view BoundedProjectionView) error {
	a.rebuilt = true
	a.view = view
	return nil
}

type batchRetentionProjectionApplier struct {
	batchCount int
	itemIDs    []string
}

func (a *batchRetentionProjectionApplier) BeginProjectionRebuild(context.Context, Scope, string) error {
	a.batchCount = 0
	a.itemIDs = nil
	return nil
}

func (a *batchRetentionProjectionApplier) ApplyProjectionRebuildBatch(_ context.Context, _ Scope, _ string, view BoundedProjectionView) error {
	if len(view.Active) > 1 {
		return errors.New("rebuild batch exceeded configured bound")
	}
	a.batchCount++
	for _, active := range view.Active {
		a.itemIDs = append(a.itemIDs, active.Item.ItemID)
	}
	return nil
}

func (a *batchRetentionProjectionApplier) EndProjectionRebuild(context.Context, Scope, string) error {
	return nil
}

func (a *batchRetentionProjectionApplier) Apply(context.Context, Scope, string, []ScopeChange, []MemoryRevision) error {
	return nil
}

func (a *batchRetentionProjectionApplier) Commit(context.Context, Scope, string) error {
	return nil
}
