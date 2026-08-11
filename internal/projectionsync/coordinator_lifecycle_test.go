package projectionsync

import (
	"context"
	"errors"
	"testing"
	"time"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func TestProjectionCoordinatorActivatesOnlyAfterAllBatchesCommit(t *testing.T) {
	scope := Scope{Key: "telegram:projection", Kind: ScopeKindPersonal}
	canonical := &projectionCanonicalStore{changes: []ScopeChange{
		{Sequence: 1, OperationID: "operation-1", RevisionIDs: []string{"revision-1"}},
		{Sequence: 2, OperationID: "operation-2", RevisionIDs: []string{"revision-2"}},
	}}
	checkpoints := &projectionCheckpointStore{}
	applier := &projectionApplier{}
	coordinator, err := NewProjectionCoordinator(canonical, checkpoints, applier, 1)
	if err != nil {
		t.Fatalf("NewProjectionCoordinator() error = %v", err)
	}
	coordinator.now = func() time.Time { return time.Date(2026, time.August, 6, 1, 0, 0, 0, time.UTC) }

	manifest, err := coordinator.Sync(context.Background(), scope, "bleve", "generation-1")
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if manifest.Status != ProjectionGenerationActive || manifest.Watermark != 2 {
		t.Fatalf("manifest = %+v, want active at watermark 2", manifest)
	}
	if got, want := applier.applyCount, 2; got != want {
		t.Fatalf("apply count = %d, want %d", got, want)
	}
	if got, want := applier.commitCount, 2; got != want {
		t.Fatalf("commit count = %d, want %d", got, want)
	}
	if checkpoints.manifest.Status != ProjectionGenerationActive || checkpoints.manifest.Watermark != 2 {
		t.Fatalf("checkpoint = %+v, want active at watermark 2", checkpoints.manifest)
	}
}

func TestProjectionCoordinatorLeavesGenerationDirtyWhenApplyFails(t *testing.T) {
	scope := Scope{Key: "telegram:projection", Kind: ScopeKindPersonal}
	canonical := &projectionCanonicalStore{changes: []ScopeChange{{Sequence: 1, OperationID: "operation-1", RevisionIDs: []string{"revision-1"}}}}
	checkpoints := &projectionCheckpointStore{}
	applier := &projectionApplier{applyErr: errors.New("index unavailable")}
	coordinator, err := NewProjectionCoordinator(canonical, checkpoints, applier, 1)
	if err != nil {
		t.Fatalf("NewProjectionCoordinator() error = %v", err)
	}

	if _, err := coordinator.Sync(context.Background(), scope, "bleve", "generation-1"); !errors.Is(err, applier.applyErr) {
		t.Fatalf("Sync() error = %v, want apply failure", err)
	}
	if checkpoints.manifest.Status != ProjectionGenerationDirty {
		t.Fatalf("checkpoint status = %q, want dirty", checkpoints.manifest.Status)
	}
	if checkpoints.activateCount != 0 || checkpoints.manifest.Watermark != 0 {
		t.Fatalf("failed generation was exposed: %+v", checkpoints.manifest)
	}
}
