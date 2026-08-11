package projectionsync

import (
	"context"
	"testing"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func TestProjectionCoordinatorRebuildsWhenRetentionFloorPassesWatermark(t *testing.T) {
	scope := Scope{Key: "telegram:retention", Kind: ScopeKindPersonal}
	canonical := &retentionProjectionStore{
		boundedProjectionStore: boundedProjectionStore{
			state:     ScopeState{SchemaVersion: CanonicalSchemaVersionV1, Scope: scope, Version: 2, ChangeSeq: 3},
			active:    []ActiveCanonicalMemory{{Item: projectionViewItem(scope, "item-1"), RevisionID: "revision-1"}},
			revisions: map[string]MemoryRevision{"revision-1": projectionViewRevision("revision-1", "item-1", 1)},
		},
		floor: 2,
	}
	checkpoints := &projectionCheckpointStore{manifest: ProjectionManifest{
		Scope: scope, ProjectionID: "bleve", GenerationID: "generation-1", Status: ProjectionGenerationActive,
		Watermark: 1, UpdatedAt: projectionViewTime(1),
	}, found: true}
	applier := &retentionProjectionApplier{}
	coordinator, err := NewProjectionCoordinator(canonical, checkpoints, applier, 2)
	if err != nil {
		t.Fatalf("NewProjectionCoordinator() error = %v", err)
	}
	manifest, err := coordinator.Sync(context.Background(), scope, "bleve", "generation-1")
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if !applier.rebuilt || manifest.Status != ProjectionGenerationActive || manifest.Watermark != 3 {
		t.Fatalf("manifest = %+v, rebuilt = %t", manifest, applier.rebuilt)
	}
	if len(applier.view.Active) != 1 || applier.view.Active[0].Item.ItemID != "item-1" {
		t.Fatalf("rebuilt view = %#v", applier.view)
	}
}

func TestProjectionCoordinatorStreamsLargeRetentionRebuildInBoundedBatches(t *testing.T) {
	scope := Scope{Key: "telegram:retention-batches", Kind: ScopeKindPersonal}
	store := &retentionProjectionStore{
		boundedProjectionStore: boundedProjectionStore{
			state: ScopeState{SchemaVersion: CanonicalSchemaVersionV1, Scope: scope, Version: 3, ChangeSeq: 4},
			active: []ActiveCanonicalMemory{
				{Item: projectionViewItem(scope, "item-1"), RevisionID: "revision-1"},
				{Item: projectionViewItem(scope, "item-2"), RevisionID: "revision-2"},
				{Item: projectionViewItem(scope, "item-3"), RevisionID: "revision-3"},
			},
			revisions: map[string]MemoryRevision{
				"revision-1": projectionViewRevision("revision-1", "item-1", 1),
				"revision-2": projectionViewRevision("revision-2", "item-2", 1),
				"revision-3": projectionViewRevision("revision-3", "item-3", 1),
			},
		},
		floor: 2,
	}
	checkpoints := &projectionCheckpointStore{}
	applier := &batchRetentionProjectionApplier{}
	coordinator, err := NewProjectionCoordinator(store, checkpoints, applier, 1)
	if err != nil {
		t.Fatalf("NewProjectionCoordinator() error = %v", err)
	}
	manifest, err := coordinator.Sync(context.Background(), scope, "bleve", "generation-1")
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if manifest.Status != ProjectionGenerationActive || manifest.Watermark != 4 {
		t.Fatalf("manifest = %+v", manifest)
	}
	if got, want := applier.batchCount, 3; got != want {
		t.Fatalf("rebuild batch count = %d, want %d", got, want)
	}
	if len(applier.itemIDs) != 3 {
		t.Fatalf("rebuilt item IDs = %#v", applier.itemIDs)
	}
}
