package projection //nolint:testpackage // Exercises shared private test fixtures within the package.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baldaworks/boyan/index/bleve"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/badger"
)

func TestProjectionRetainsActiveAndPrevious(t *testing.T) {
	root := t.TempDir()

	store, err := badger.OpenCanonicalStore(filepath.Join(root, "canonical"))
	requireNoError(t, err, "%v")

	defer store.Close()

	reader, err := badger.NewCanonicalReader(store)
	requireNoError(t, err, "%v")

	index, err := bleve.Open(filepath.Join(root, "index"))
	requireNoError(t, err, "%v")

	applier, err := bleve.NewBleveCanonicalApplier(index, reader)
	requireNoError(t, err, "%v")

	defer applier.Close()

	runtime, err := NewProjectionRuntime(store, store, applier, "test", 1)
	requireNoError(t, err, "%v")

	scope := memory.Scope{Kind: memory.ScopeKindPersonal, Key: "test:retention"}

	other := memory.Scope{Kind: memory.ScopeKindGroup, Key: scope.Key}
	for _, scope := range []memory.Scope{scope, other} {
		requireNoError(t, runtime.Sync(context.Background(), scope), "%v")
	}

	stopSearch, searchErrors := startRetentionSearch(index, other)
	defer stopSearch()

	for range 5 {
		active, _, err := store.LoadActiveProjectionManifest(context.Background(), scope, "test")
		requireNoError(t, err, "%v")

		requireNoError(t, store.MarkProjectionDirty(context.Background(), active), "%v")

		requireNoError(t, runtime.Sync(context.Background(), scope), "%v")
	}

	entries, err := os.ReadDir(filepath.Join(root, "index", "generations"))
	requireNoError(t, err, "%v")

	if len(entries) > 3 {
		t.Fatalf(
			"retained %d generation directories, want at most two plus other scope",
			len(entries),
		)
	}

	if _, found, err := store.LoadActiveProjectionManifest(context.Background(), other, "test"); err != nil || !found {
		t.Fatalf("other scope lost: %v %v", found, err)
	}

	select {
	case err := <-searchErrors:
		t.Fatalf("search during retention: %v", err)
	default:
	}
}

func TestProjectionRecoversMissingActiveIndex(t *testing.T) {
	root := t.TempDir()

	store, err := badger.OpenCanonicalStore(filepath.Join(root, "canonical"))
	requireNoError(t, err, "%v")

	defer store.Close()

	reader, err := badger.NewCanonicalReader(store)
	requireNoError(t, err, "%v")

	open := func() (*ProjectionRuntime, *bleve.BleveCanonicalApplier) {
		index, err := bleve.Open(filepath.Join(root, "index"))
		requireNoError(t, err, "%v")

		applier, err := bleve.NewBleveCanonicalApplier(index, reader)
		requireNoError(t, err, "%v")

		runtime, err := NewProjectionRuntime(store, store, applier, "test", 1)
		requireNoError(t, err, "%v")

		return runtime, applier
	}
	runtime, applier := open()

	scope := memory.Scope{Kind: memory.ScopeKindPersonal, Key: "test:missing"}
	requireNoError(t, runtime.Sync(context.Background(), scope), "%v")

	first, _, err := store.LoadActiveProjectionManifest(context.Background(), scope, "test")
	requireNoError(t, err, "%v")

	requireNoError(t, applier.Close(), "%v")

	requireNoError(t, os.RemoveAll(filepath.Join(root, "index", "generations", first.GenerationID)), "%v")

	runtime, applier = open()
	defer applier.Close()

	requireNoError(t, runtime.Start(context.Background()), "%v")

	requireNoError(t, runtime.Sync(context.Background(), scope), "%v")

	active, found, err := store.LoadActiveProjectionManifest(context.Background(), scope, "test")
	if err != nil || !found || active.GenerationID == first.GenerationID {
		t.Fatalf("missing index recovery: %+v %v", active, err)
	}
}

func TestProjectionStartupReconcilesBeforePruning(t *testing.T) {
	root := t.TempDir()

	store, err := badger.OpenCanonicalStore(filepath.Join(root, "canonical"))
	requireNoError(t, err, "%v")

	defer store.Close()

	reader, err := badger.NewCanonicalReader(store)
	requireNoError(t, err, "%v")

	open := func() (*ProjectionRuntime, *bleve.BleveCanonicalApplier) {
		index, err := bleve.Open(filepath.Join(root, "index"))
		requireNoError(t, err, "%v")

		applier, err := bleve.NewBleveCanonicalApplier(index, reader)
		requireNoError(t, err, "%v")

		runtime, err := NewProjectionRuntime(store, store, applier, "test", 1)
		requireNoError(t, err, "%v")

		return runtime, applier
	}
	runtime, applier := open()

	scope := memory.Scope{Kind: memory.ScopeKindPersonal, Key: "test:reconcile"}
	requireNoError(t, runtime.Sync(context.Background(), scope), "%v")

	first, _, err := store.LoadActiveProjectionManifest(context.Background(), scope, "test")
	requireNoError(t, err, "%v")

	requireNoError(t, applier.BeginProjectionRebuild(context.Background(), scope, "abandoned"), "%v")

	requireNoError(t, applier.BeginProjectionRebuild(context.Background(), scope, "committed-future"), "%v")

	requireNoError(t, applier.Commit(context.Background(), scope, "committed-future"), "%v")

	future := first

	future.GenerationID = "committed-future"
	requireNoError(t, store.MarkProjectionDirty(context.Background(), future), "%v")

	requireNoError(t, store.ActivateProjectionGeneration(context.Background(), future, time.Now().UTC()), "%v")

	requireNoError(t, applier.Close(), "%v")

	outside := t.TempDir()

	sentinel := filepath.Join(outside, "keep")
	requireNoError(t, os.WriteFile(sentinel, []byte("safe"), 0600), "%v")

	requireNoError(t, os.Symlink(outside, filepath.Join(root, "index", "generations", "external-link")), "%v")

	runtime, applier = open()
	defer applier.Close()

	requireNoError(t, runtime.Start(context.Background()), "%v")

	assertReconciledProjectionRetention(t, root, sentinel, future, scope, applier)
}

func startRetentionSearch(index *bleve.BleveRecallProjection, other memory.Scope) (func(), <-chan error) {
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

			if _, err := index.SearchRecall(
				context.Background(),
				memory.RecallRequest{
					Scope: other,
					Query: "test",
					Limit: 1,
				},
			); err != nil {
				searchErrors <- err

				return
			}
		}
	}()

	return func() { close(stopSearch); <-searchDone }, searchErrors
}

func assertReconciledProjectionRetention(
	t *testing.T,
	root,
	sentinel string,
	future memory.ProjectionManifest,
	scope memory.Scope,
	applier *bleve.BleveCanonicalApplier,
) {
	t.Helper()

	markers, err := filepath.Glob(filepath.Join(root, "index", ".active-generation.scope.*"))
	if err != nil || len(markers) != 1 {
		t.Fatalf("markers: %v %v", markers, err)
	}

	marker, err := os.ReadFile(markers[0])
	if err != nil || strings.TrimSpace(string(marker)) != future.GenerationID {
		t.Fatalf("unreconciled marker: %q %v", marker, err)
	}

	if _, err := os.Stat(filepath.Join(root, "index", "generations", "abandoned")); !os.IsNotExist(err) {
		t.Fatalf("abandoned generation retained: %v", err)
	}

	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("symlink target was touched: %v", err)
	}

	requireNoError(t, applier.BeginProjectionRebuild(context.Background(), scope, "inflight"), "%v")

	requireNoError(t, applier.ReconcileProjection(context.Background()), "%v")

	if _, err := os.Stat(filepath.Join(root, "index", "generations", "inflight")); err != nil {
		t.Fatalf("in-flight generation pruned: %v", err)
	}

	requireNoError(t, applier.AbortProjectionSync(context.Background(), "inflight"), "%v")

	if _, err := os.Stat(filepath.Join(root, "index", "generations", "inflight")); !os.IsNotExist(err) {
		t.Fatalf("failed builder retained: %v", err)
	}
}
