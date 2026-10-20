package projection //nolint:testpackage // Exercises shared private test fixtures within the package.

import (
	"context"
	"errors"

	"sync"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

type scanTrackingStore struct {
	memory.CanonicalStore

	mu     sync.Mutex
	starts []uint64
}

func (s *scanTrackingStore) ScanScopeChanges(
	ctx context.Context,
	scope memory.Scope,
	after uint64,
	limit uint32,
) (
	[]memory.ScopeChange,
	error,
) {
	s.mu.Lock()
	s.starts = append(s.starts, after)
	s.mu.Unlock()

	return s.CanonicalStore.ScanScopeChanges(ctx, scope, after, limit)
}

func projectionMutation(scope memory.Scope, version uint64, id string) memory.CanonicalMutation {
	now := time.Now().UTC()

	return memory.CanonicalMutation{SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: scope,
		ExpectedScopeVersion: version,
		Operation:            memory.OperationRecord{OperationID: id, Fingerprint: id, CommittedAt: now},
		Items:                []memory.MemoryItem{{ItemID: id, Scope: scope, Kind: memory.MemoryKindEvent}},
		Revisions: []memory.MemoryRevision{{SchemaVersion: memory.MemorySchemaVersion, RevisionID: id,
			ItemID: id, Revision: 1,
			Temporal: memory.Temporal{ObservedAt: now}, Evidence: []memory.EvidenceRef{
				{
					SourceID:      "source-1",
					MessageID:     "message-1",
					Role:          memory.MessageRoleUser,
					StartByte:     1,
					EndByte:       2,
					AssertionMode: memory.AssertionModeUser,
				},
			}, Sensitivity: memory.SensitivityStandard, Retention: memory.RetentionClassStandard, Payload: memory.PayloadRef{
				ID:       "payload-1",
				Digest:   "digest-1",
				ByteSize: 1,
			}}},
		Heads: []memory.ItemHead{{ItemID: id, RevisionID: id}}}
}

func TestProjectionSyncResumesCommittedProgress(t *testing.T) {
	runProjectionAuthorities(t, func(t *testing.T, newCanonical projectionOwnerFactory) {
		root := t.TempDir()

		store, reader := func() (projectionTestOwner, memory.RecallCanonicalReader) {
			return newCanonical(t)
		}()
		defer store.Close()

		tracked := &scanTrackingStore{CanonicalStore: store}
		openRuntime := func() (*ProjectionRuntime, *projectionSQLApplier) {
			applier := openProjectionSQLite(t, root, store, reader)

			runtime, err := NewProjectionRuntime(tracked, store, applier, "sqlite", 1)
			requireNoError(t, err, "%v")

			return runtime, applier
		}
		scope := memory.Scope{Kind: memory.ScopeKindPersonal, Key: "test:progress"}

		runtime, applier := openRuntime()
		defer func() { _ = runtime.Close(context.Background()); _ = applier.Close() }()

		if _, err := store.ApplyCanonicalMutation(context.Background(), projectionMutation(scope, 0, "one")); err != nil {
			t.Fatal(err)
		}

		requireNoError(t, runtime.Sync(context.Background(), scope), "%v")

		first, _, err := store.LoadActiveProjectionManifest(context.Background(), scope, "sqlite")
		requireNoError(t, err, "%v")

		requireNoError(t, runtime.Sync(context.Background(), scope), "%v")

		unchanged, _, err := store.LoadActiveProjectionManifest(context.Background(), scope, "sqlite")
		requireNoError(t, err, "%v")

		if unchanged.GenerationID != first.GenerationID ||
			!unchanged.UpdatedAt.Equal(first.UpdatedAt) {
			t.Error("unchanged sync rebuilt generation")
		}

		requireNoError(t, runtime.Close(context.Background()), "%v")

		requireNoError(t, applier.Close(), "%v")

		runtime, applier = openRuntime()
		assertResumedProjectionDelta(t, runtime, tracked, store, scope, first)
	})
}

type failedCommitApplier struct {
	*projectionSQLApplier

	fail bool
}

func (a *failedCommitApplier) CommitProjectionManifest(ctx context.Context, manifest memory.ProjectionManifest) error {
	if err := a.projectionSQLApplier.CommitProjectionManifest(ctx, manifest); err != nil {
		return err
	}

	if a.fail {
		return errors.New("injected failure after physical commit")
	}

	return nil
}

func TestProjectionSyncRecoversDirtyAfterRestart(t *testing.T) {
	runProjectionAuthorities(t, func(t *testing.T, newCanonical projectionOwnerFactory) {
		root := t.TempDir()

		store, reader := func() (projectionTestOwner, memory.RecallCanonicalReader) {
			return newCanonical(t)
		}()
		defer store.Close()

		runtime, applier := openDirtyProjectionRuntime(t, root, store, reader)
		defer func() { _ = runtime.Close(context.Background()); _ = applier.Close() }()

		scope := memory.Scope{Kind: memory.ScopeKindPersonal, Key: "test:dirty"}

		other := memory.Scope{Kind: memory.ScopeKindGroup, Key: scope.Key}
		for _, scope := range []memory.Scope{scope, other} {
			if _, err := store.ApplyCanonicalMutation(context.Background(), projectionMutation(scope, 0, "one")); err != nil {
				t.Fatal(err)
			}

			requireNoError(t, runtime.Sync(context.Background(), scope), "%v")
		}

		first, _, err := store.LoadActiveProjectionManifest(context.Background(), scope, "sqlite")
		requireNoError(t, err, "%v")

		foreign, _, err := store.LoadActiveProjectionManifest(context.Background(), other, "sqlite")
		requireNoError(t, err, "%v")

		if _, err := store.ApplyCanonicalMutation(context.Background(), projectionMutation(scope, 1, "two")); err != nil {
			t.Fatal(err)
		}

		applier.fail = true

		if err := runtime.Sync(context.Background(), scope); err == nil {
			t.Fatal("injected commit failure was ignored")
		}

		assertDirtyProjectionManifest(t, store, scope, first)

		requireNoError(t, runtime.Close(context.Background()), "%v")

		requireNoError(t, applier.Close(), "%v")

		runtime, applier = openDirtyProjectionRuntime(t, root, store, reader)
		requireNoError(t, runtime.Sync(context.Background(), scope), "%v")

		assertProjectionRecoveredAfterRestart(t, runtime, store, scope, other, first, foreign)
	})
}

type replayOnlyApplier struct{ commits int }

func (*replayOnlyApplier) Apply(
	context.Context,
	memory.Scope,
	string,
	[]memory.ScopeChange,
	[]memory.MemoryRevision,
) error {
	return nil
}
func (a *replayOnlyApplier) Commit(context.Context, memory.Scope, string) error {
	a.commits++

	return nil
}

func TestProjectionSyncSupportsReplayOnlyApplier(t *testing.T) {
	runProjectionAuthorities(t, func(t *testing.T, newCanonical projectionOwnerFactory) {
		store := func() projectionTestOwner {
			owner, _ := newCanonical(t)

			return owner
		}()

		defer store.Close()

		applier := &replayOnlyApplier{}

		runtime, err := NewProjectionRuntime(store, store, applier, "sqlite", 1)
		requireNoError(t, err, "%v")

		requireNoError(t, runtime.Sync(
			context.Background(),
			memory.Scope{Kind: memory.ScopeKindPersonal, Key: "test:portable"},
		), "%v")

		if applier.commits == 0 {
			t.Fatal("projection was never committed")
		}
	})
}

func assertConcurrentProjectionNoop(
	t *testing.T,
	runtime *ProjectionRuntime,
	store projectionTestOwner,
	scope memory.Scope,
	recovered memory.ProjectionManifest,
) {
	t.Helper()

	var group sync.WaitGroup

	failures := make(chan error, 10)

	for range 10 {
		group.Go(func() { failures <- runtime.Sync(context.Background(), scope) })
	}

	group.Wait()
	close(failures)

	for err := range failures {
		requireNoError(t, err, "%v")
	}

	final, _, err := store.LoadActiveProjectionManifest(context.Background(), scope, "sqlite")
	if err != nil || final != recovered {
		t.Fatalf("concurrent unchanged sync: %+v %v", final, err)
	}
}

func assertResumedProjectionDelta(
	t *testing.T,
	runtime *ProjectionRuntime,
	tracked *scanTrackingStore,
	store projectionTestOwner,
	scope memory.Scope,
	first memory.ProjectionManifest,
) {
	t.Helper()

	tracked.starts = nil

	if _, err := store.ApplyCanonicalMutation(context.Background(), projectionMutation(scope, 1, "two")); err != nil {
		t.Fatal(err)
	}

	requireNoError(t, runtime.Sync(context.Background(), scope), "%v")

	second, _, err := store.LoadActiveProjectionManifest(context.Background(), scope, "sqlite")
	requireNoError(t, err, "%v")

	if second.GenerationID != first.GenerationID || second.Watermark != 2 {
		t.Errorf("delta manifest: %+v first: %+v", second, first)
	}

	if len(tracked.starts) == 0 || tracked.starts[0] != 1 {
		t.Errorf("restart scan starts: %v, want initial 1", tracked.starts)
	}
}

func assertDirtyProjectionManifest(
	t *testing.T,
	store projectionTestOwner,
	scope memory.Scope,
	first memory.ProjectionManifest,
) {
	t.Helper()

	dirty, found, err := store.LoadProjectionManifest(context.Background(), scope, "sqlite", first.GenerationID)
	if err != nil || !found || dirty.Status != memory.ProjectionGenerationDirty ||
		dirty.Watermark != 1 {
		t.Fatalf("dirty manifest: %+v %v", dirty, err)
	}
}

func assertProjectionRecoveredAfterRestart(
	t *testing.T,
	runtime *ProjectionRuntime,
	store projectionTestOwner,
	scope,
	other memory.Scope,
	first,
	foreign memory.ProjectionManifest,
) {
	t.Helper()

	recovered, _, err := store.LoadActiveProjectionManifest(context.Background(), scope, "sqlite")
	requireNoError(t, err, "%v")

	if recovered.GenerationID == first.GenerationID || recovered.Watermark != 2 {
		t.Fatalf("recovery: %+v", recovered)
	}

	preserved, _, err := store.LoadActiveProjectionManifest(context.Background(), other, "sqlite")
	requireNoError(t, err, "%v")

	if preserved != foreign {
		t.Fatalf("other scope manifest changed: %+v", preserved)
	}

	assertConcurrentProjectionNoop(t, runtime, store, scope, recovered)
}

func openDirtyProjectionRuntime(
	t *testing.T,
	root string,
	store projectionTestOwner,
	reader memory.RecallCanonicalReader,
) (*ProjectionRuntime, *failedCommitApplier) {
	t.Helper()

	actual := openProjectionSQLite(t, root, store, reader)

	applier := &failedCommitApplier{projectionSQLApplier: actual}

	runtime, err := NewProjectionRuntime(store, store, applier, "sqlite", 1)
	requireNoError(t, err, "%v")

	return runtime, applier
}
