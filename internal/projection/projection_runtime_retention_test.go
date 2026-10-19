package projection //nolint:testpackage // Exercises shared SQL/canonical retention fixtures.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	index "github.com/baldaworks/boyan/index/sqlite"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/badger"
)

func TestProjectionRetainsActiveAndPrevious(t *testing.T) {
	runProjectionAuthorities(t, func(t *testing.T, newCanonical projectionOwnerFactory) {
		root := t.TempDir()

		store, reader := func() (projectionTestOwner, memory.RecallCanonicalReader) {
			if newCanonical != nil {
				return newCanonical(t)
			}

			store, err := badger.OpenCanonicalStore(filepath.Join(root, "canonical"))
			requireNoError(t, err, "%v")
			reader, err := badger.NewCanonicalReader(store)
			requireNoError(t, err, "%v")

			return store, reader
		}()
		defer store.Close()

		applier := openProjectionSQLite(t, root, store, reader)
		defer applier.Close()

		runtime, err := NewProjectionRuntime(store, store, applier, "sqlite", 1)
		requireNoError(t, err, "%v")

		defer runtime.Close(context.Background())

		scope := memory.Scope{Kind: memory.ScopeKindPersonal, Key: "test:retention"}

		other := memory.Scope{Kind: memory.ScopeKindGroup, Key: scope.Key}
		for _, scope := range []memory.Scope{scope, other} {
			requireNoError(t, runtime.Sync(context.Background(), scope), "%v")
		}

		stopSearch, searchErrors := startRetentionSearch(applier.store, other)
		defer stopSearch()

		for range 5 {
			active, _, err := store.LoadActiveProjectionManifest(context.Background(), scope, "sqlite")
			requireNoError(t, err, "%v")
			requireNoError(t, store.MarkProjectionDirty(context.Background(), active), "%v")
			requireNoError(t, runtime.Sync(context.Background(), scope), "%v")
		}

		var builds int
		requireNoError(t, applier.db.QueryRowContext(t.Context(), "SELECT count(*) FROM lexical_build").Scan(&builds), "%v")

		if builds > 3 {
			t.Fatalf("retained %d builds, want at most active+previous plus other scope", builds)
		}

		if _, found, err := store.LoadActiveProjectionManifest(context.Background(), other, "sqlite"); err != nil || !found {
			t.Fatalf("other scope lost: %v %v", found, err)
		}

		select {
		case err := <-searchErrors:
			t.Fatalf("search during retention: %v", err)
		default:
		}
	})
}

func TestProjectionRecoversMissingActiveIndex(t *testing.T) {
	runProjectionAuthorities(t, func(t *testing.T, newCanonical projectionOwnerFactory) {
		root := t.TempDir()

		store, reader := func() (projectionTestOwner, memory.RecallCanonicalReader) {
			if newCanonical != nil {
				return newCanonical(t)
			}

			store, err := badger.OpenCanonicalStore(filepath.Join(root, "canonical"))
			requireNoError(t, err, "%v")
			reader, err := badger.NewCanonicalReader(store)
			requireNoError(t, err, "%v")

			return store, reader
		}()
		defer store.Close()

		open := func() (*ProjectionRuntime, *projectionSQLApplier) {
			applier := openProjectionSQLite(t, root, store, reader)
			runtime, err := NewProjectionRuntime(store, store, applier, "sqlite", 1)
			requireNoError(t, err, "%v")

			return runtime, applier
		}
		runtime, applier := open()
		scope := memory.Scope{Kind: memory.ScopeKindPersonal, Key: "test:missing"}
		requireNoError(t, runtime.Sync(context.Background(), scope), "%v")
		first, _, err := store.LoadActiveProjectionManifest(context.Background(), scope, "sqlite")
		requireNoError(t, err, "%v")
		requireNoError(t, runtime.Close(context.Background()), "%v")
		requireNoError(t, applier.Close(), "%v")
		requireNoError(t, os.Remove(filepath.Join(root, "index", "search", "search.sqlite")), "%v")

		runtime, applier = open()
		defer applier.Close()
		defer runtime.Close(context.Background())

		requireNoError(t, runtime.Start(context.Background()), "%v")
		requireNoError(t, runtime.Sync(context.Background(), scope), "%v")

		active, found, err := store.LoadActiveProjectionManifest(context.Background(), scope, "sqlite")
		if err != nil || !found || active.GenerationID == first.GenerationID {
			t.Fatalf("missing index recovery: %+v %v", active, err)
		}
	})
}

//nolint:funlen // Reopen, canonical-first activation and pruning share one crash-recovery contract.
func TestProjectionStartupReconcilesBeforePruning(t *testing.T) {
	runProjectionAuthorities(t, func(t *testing.T, newCanonical projectionOwnerFactory) {
		root := t.TempDir()

		store, reader := func() (projectionTestOwner, memory.RecallCanonicalReader) {
			if newCanonical != nil {
				return newCanonical(t)
			}

			store, err := badger.OpenCanonicalStore(filepath.Join(root, "canonical"))
			requireNoError(t, err, "%v")
			reader, err := badger.NewCanonicalReader(store)
			requireNoError(t, err, "%v")

			return store, reader
		}()
		defer store.Close()

		open := func() (*ProjectionRuntime, *projectionSQLApplier) {
			applier := openProjectionSQLite(t, root, store, reader)
			runtime, err := NewProjectionRuntime(store, store, applier, "sqlite", 1)
			requireNoError(t, err, "%v")

			return runtime, applier
		}
		runtime, applier := open()
		scope := memory.Scope{Kind: memory.ScopeKindPersonal, Key: "test:reconcile"}
		requireNoError(t, runtime.Sync(context.Background(), scope), "%v")
		first, _, err := store.LoadActiveProjectionManifest(context.Background(), scope, "sqlite")
		requireNoError(t, err, "%v")
		requireNoError(t, applier.BeginProjectionRebuild(context.Background(), scope, "abandoned"), "%v")
		// Reopen to leave a real unreachable crashed owner, rather than adopting an
		// in-process producer's stage as a second build.
		requireNoError(t, runtime.Close(context.Background()), "%v")
		requireNoError(t, applier.Close(), "%v")
		runtime, applier = open()
		requireNoError(t, applier.BeginProjectionRebuild(context.Background(), scope, "committed-future"), "%v")

		future := first
		future.GenerationID = "committed-future"
		future.Status = memory.ProjectionGenerationDirty
		requireNoError(t, applier.CommitProjectionManifest(context.Background(), future), "%v")
		requireNoError(t, store.MarkProjectionDirty(context.Background(), future), "%v")
		requireNoError(t, store.ActivateProjectionGeneration(context.Background(), future, time.Now().UTC()), "%v")
		requireNoError(t, runtime.Close(context.Background()), "%v")
		requireNoError(t, applier.Close(), "%v")

		runtime, applier = open()
		defer applier.Close()
		defer runtime.Close(context.Background())

		requireNoError(t, runtime.Start(context.Background()), "%v")

		if active := applier.activeManifest(t, scope); active.GenerationID != future.GenerationID {
			t.Fatalf("canonical-first activation not reconciled: %+v", active)
		}

		if applier.countGeneration(t, "abandoned") != 0 {
			t.Fatal("abandoned generation retained")
		}

		requireNoError(t, applier.BeginProjectionRebuild(context.Background(), scope, "inflight"), "%v")
		requireNoError(t, applier.ReconcileProjection(context.Background()), "%v")

		if applier.countGeneration(t, "inflight") != 1 {
			t.Fatal("in-flight generation pruned")
		}

		requireNoError(t, applier.AbortProjectionSync(context.Background(), "inflight"), "%v")

		if applier.countGeneration(t, "inflight") != 0 {
			t.Fatal("failed builder retained")
		}
	})
}

func startRetentionSearch(store *index.Store, other memory.Scope) (func(), <-chan error) {
	stopSearch := make(chan struct{})
	searchDone := make(chan struct{})
	searchErrors := make(chan error, 1)

	go func() {
		defer close(searchDone)

		for {
			select {
			case <-stopSearch:
				return
			default:
			}

			request := memory.RecallRequest{Scope: other, Query: "test", Limit: 1}
			if _, err := store.SearchRecall(context.Background(), request); err != nil {
				searchErrors <- err

				return
			}
		}
	}()

	return func() { close(stopSearch); <-searchDone }, searchErrors
}
