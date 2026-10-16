//nolint:cyclop,funlen,lll // Explicit path fixtures enumerate independently derived replay targets.
package projectionsync //nolint:testpackage // Exercises coordinator boundary fixtures.

import (
	"context"
	"errors"
	"reflect"
	"testing"

	. "github.com/baldaworks/boyan/pkg/memory"
)

type manifestProjectionApplier struct {
	batchRetentionProjectionApplier

	manifests []ProjectionManifest
	err       error
}

func (a *manifestProjectionApplier) Commit(context.Context, Scope, string) error {
	return errors.New("legacy commit selected for manifest-capable applier")
}

func (a *manifestProjectionApplier) CommitProjectionManifest(_ context.Context, manifest ProjectionManifest) error {
	a.manifests = append(a.manifests, manifest)

	return a.err
}

type advancingProjectionStore struct {
	retentionProjectionStore

	loads int
}

func (s *advancingProjectionStore) LoadScopeState(context.Context, Scope) (ScopeState, error) {
	s.loads++

	state := s.state
	if s.loads > 1 {
		state.ChangeSeq = 9
	}

	return state, nil
}

// Replacing any physical commit with the legacy method, deriving a rebuild
// anchor from newer pages, or sealing the old checkpoint fails this matrix.
func TestCoordinatorCommitsExactManifestAtEveryPath(t *testing.T) {
	scope := Scope{Key: "coordinator:manifest", Kind: ScopeKindPersonal}

	for _, tc := range []struct {
		name       string
		rebuild    bool
		watermark  uint64
		floor      uint64
		state      uint64
		changes    []ScopeChange
		concurrent bool
		want       []uint64
	}{
		{name: "each replay batch", changes: []ScopeChange{{Sequence: 1}, {Sequence: 2}}, want: []uint64{1, 2}},
		{name: "empty incremental full uint64", watermark: 18446744073709551615, want: []uint64{18446744073709551615}},
		{name: "empty rebuild full uint64", rebuild: true, state: 18446744073709551615, want: []uint64{18446744073709551615, 18446744073709551615}},
		{name: "retention rebuild", watermark: 1, floor: 3, state: 7, want: []uint64{7, 7}},
		{name: "rebuild captures initial anchor", rebuild: true, state: 7, concurrent: true,
			changes: []ScopeChange{{Sequence: 9}}, want: []uint64{7, 9}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &advancingProjectionStore{retentionProjectionStore: retentionProjectionStore{
				boundedProjectionStore: boundedProjectionStore{
					state:   ScopeState{SchemaVersion: CanonicalSchemaVersionV1, Scope: scope, ChangeSeq: tc.state},
					changes: tc.changes,
				}, floor: tc.floor,
			}}
			if tc.concurrent {
				store.active = []ActiveCanonicalMemory{{Item: projectionViewItem(scope, "item"), RevisionID: "r"}}
				store.revisions = map[string]MemoryRevision{"r": projectionViewRevision("r", "item", 9)}
			}

			checkpoints := &projectionCheckpointStore{manifest: ProjectionManifest{
				Scope: scope, ProjectionID: "sqlite", GenerationID: "g", Status: ProjectionGenerationDirty,
				Watermark: tc.watermark, UpdatedAt: projectionViewTime(1),
			}, found: true}
			applier := &manifestProjectionApplier{}
			coordinator, err := NewProjectionCoordinator(store, checkpoints, applier, 1)
			requireNoError(t, err, "coordinator: %v")

			var got ProjectionManifest
			if tc.rebuild {
				got, err = coordinator.Rebuild(t.Context(), scope, "sqlite", "g")
			} else {
				got, err = coordinator.Sync(t.Context(), scope, "sqlite", "g")
			}

			requireNoError(t, err, "sync/rebuild: %v")

			var watermarks []uint64

			for _, manifest := range applier.manifests {
				if manifest.Scope != scope || manifest.ProjectionID != "sqlite" || manifest.GenerationID != "g" || manifest.Status != ProjectionGenerationDirty {
					t.Fatalf("physical target = %+v", manifest)
				}

				watermarks = append(watermarks, manifest.Watermark)
			}

			if !reflect.DeepEqual(watermarks, tc.want) {
				t.Fatalf("physical watermarks = %v, want %v", watermarks, tc.want)
			}

			if got.Status != ProjectionGenerationActive || got.Watermark != tc.want[len(tc.want)-1] {
				t.Fatalf("published manifest = %+v", got)
			}
		})
	}
}

func TestCoordinatorManifestFailureNeverAdvancesOrActivates(t *testing.T) {
	for _, path := range []string{"empty", "replay", "rebuild", "retention"} {
		t.Run(path, func(t *testing.T) {
			scope := Scope{Key: "coordinator:failure", Kind: ScopeKindPersonal}

			store := &retentionProjectionStore{boundedProjectionStore: boundedProjectionStore{
				state: ScopeState{SchemaVersion: CanonicalSchemaVersionV1, Scope: scope, ChangeSeq: 7},
			}}
			if path == "replay" {
				store.changes = []ScopeChange{{Sequence: 7}}
			}

			if path == "retention" {
				store.floor = 2
			}

			checkpoints := &projectionCheckpointStore{}
			fault := errors.New("physical seal failure")
			applier := &manifestProjectionApplier{err: fault}
			coordinator, err := NewProjectionCoordinator(store, checkpoints, applier, 1)
			requireNoError(t, err, "coordinator: %v")

			if path == "rebuild" {
				_, err = coordinator.Rebuild(t.Context(), scope, "sqlite", "g")
			} else {
				_, err = coordinator.Sync(t.Context(), scope, "sqlite", "g")
			}

			if !errors.Is(err, fault) {
				t.Fatalf("error = %v, want seal failure", err)
			}

			if checkpoints.manifest.Status != ProjectionGenerationDirty || checkpoints.manifest.Watermark != 0 || checkpoints.activateCount != 0 {
				t.Fatalf("failed physical target published: %+v", checkpoints.manifest)
			}
		})
	}
}
