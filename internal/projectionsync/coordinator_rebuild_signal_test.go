package projectionsync //nolint:testpackage // Verifies durable rebuild markers cannot be certified as deltas.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func TestProjectionCoordinatorStopsBeforeRebuildRequiredBatch(t *testing.T) {
	var change ScopeChange
	requireNoError(t, json.Unmarshal([]byte(`{"sequence":1,"operation_id":"forget-large","requires_rebuild":true}`),
		&change), "%v")
	canonical := &projectionCanonicalStore{changes: []ScopeChange{change}}
	checkpoints := &projectionCheckpointStore{}
	applier := &projectionApplier{}
	coordinator, err := NewProjectionCoordinator(canonical, checkpoints, applier, 1)
	requireNoError(t, err, "%v")

	_, err = coordinator.Sync(
		context.Background(), Scope{Key: "test:marker", Kind: ScopeKindPersonal}, "test", "generation",
	)
	if !errors.Is(err, ErrProjectionRebuildRequired) {
		t.Fatal("rebuild-required change certified without rebuilding")
	}

	if applier.applyCount != 0 || applier.commitCount != 0 || checkpoints.manifest.Watermark != 0 ||
		checkpoints.activateCount != 0 || checkpoints.manifest.Status != ProjectionGenerationDirty {
		t.Fatalf("marker advanced projection: applier=%+v checkpoint=%+v", applier, checkpoints)
	}
}

func TestProjectionRebuildRejectsMarkerNewerThanSnapshot(t *testing.T) {
	for _, newer := range []bool{false, true} {
		t.Run(map[bool]string{false: "included", true: "concurrent"}[newer], func(t *testing.T) {
			scope := Scope{Key: "test:marker", Kind: ScopeKindPersonal}

			sequence := uint64(1)
			if newer {
				sequence++
			}

			canonical := &boundedProjectionStore{
				state: ScopeState{
					SchemaVersion: CanonicalSchemaVersionV1, Scope: scope, Version: 1, ChangeSeq: 1,
				},
				changes: []ScopeChange{{
					Sequence: sequence, OperationID: "forget-large", RequiresRebuild: true,
				}},
			}
			checkpoints := &projectionCheckpointStore{}
			applier := &retentionProjectionApplier{}
			coordinator, err := NewProjectionCoordinator(canonical, checkpoints, applier, 1)
			requireNoError(t, err, "%v")

			manifest, err := coordinator.Rebuild(context.Background(), scope, "test", "generation")
			if newer {
				if !errors.Is(err, ErrProjectionRebuildRequired) || checkpoints.activateCount != 0 ||
					checkpoints.manifest.Status != ProjectionGenerationDirty ||
					checkpoints.manifest.Watermark != 1 {
					t.Fatalf("newer marker certified: manifest=%+v err=%v", manifest, err)
				}

				return
			}

			requireNoError(t, err, "%v")

			if manifest.Status != ProjectionGenerationActive || manifest.Watermark != 1 {
				t.Fatalf("snapshot marker not covered: %+v", manifest)
			}
		})
	}
}
