package projectionsync //nolint:testpackage // Exercises the actual coordinator with public SQL owners.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

type coordinatorSQLOwner interface {
	memory.CanonicalStore
	memory.CanonicalOperationCommitter
	memory.ProjectionCheckpointStore
	memory.ProjectionRetentionFloorWriter
	PrepareSourcePrivacy(ctx context.Context) error
}

//nolint:cyclop // Real SQL recovery and rebuild assertions share one durable lifecycle.
func TestCoordinatorSQLAuthorityCommitFailureAndRetentionRebuild(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, fixture *storefixture.Fixture) {
		store := fixture.Store.(coordinatorSQLOwner)
		requireNoError(t, store.PrepareSourcePrivacy(t.Context()), "%v")

		scope := memory.Scope{Key: "test:sql-coordinator", Kind: memory.ScopeKindPersonal}
		for i, id := range []string{"one", "two"} {
			_, err := store.CommitCanonicalOperation(t.Context(), memory.CanonicalOperationCommitRequest{Scope: scope,
				ExpectedScopeVersion: uint64(i), OperationID: id, Fingerprint: id, CommittedAt: time.Now().UTC()})
			requireNoError(t, err, "%v")
		}

		applier := &manifestProjectionApplier{err: errors.New("physical commit unavailable")}
		coordinator, err := NewProjectionCoordinator(store, store, applier, 1)
		requireNoError(t, err, "%v")

		if _, err := coordinator.Sync(t.Context(), scope, "sqlite", "first"); !errors.Is(err, applier.err) {
			t.Fatalf("commit failure=%v", err)
		}

		manifest, found, err := store.LoadProjectionManifest(t.Context(), scope, "sqlite", "first")
		if err != nil || !found || manifest.Status != memory.ProjectionGenerationDirty || manifest.Watermark != 0 {
			t.Fatalf("failed=%+v %t %v", manifest, found, err)
		}

		applier.err = nil
		manifest, err = coordinator.Sync(t.Context(), scope, "sqlite", "first")
		requireNoError(t, err, "%v")

		if manifest.Status != memory.ProjectionGenerationActive || manifest.Watermark != 2 {
			t.Fatalf("replay=%+v", manifest)
		}

		requireNoError(t, store.SetProjectionRetentionFloor(t.Context(), scope, "sqlite", 2), "%v")
		manifest, err = coordinator.Sync(t.Context(), scope, "sqlite", "replacement")
		requireNoError(t, err, "%v")

		if manifest.Status != memory.ProjectionGenerationActive || manifest.Watermark != 2 || applier.batchCount != 1 {
			t.Fatalf("rebuild=%+v batches=%d", manifest, applier.batchCount)
		}

		old, found, err := store.LoadProjectionManifest(t.Context(), scope, "sqlite", "first")
		if err != nil || !found || old.Status != memory.ProjectionGenerationSuperseded {
			t.Fatalf("retired=%+v %v", old, err)
		}

		state, err := store.LoadScopeState(t.Context(), scope)
		if err != nil || state.Version != 2 || state.ChangeSeq != 2 {
			t.Fatalf("history=%+v %v", state, err)
		}
	})
}
