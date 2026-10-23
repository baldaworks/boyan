//go:build integration

package canonicalcore_test

import (
	"context"
	"os"
	"testing"

	index "github.com/baldaworks/boyan/index/sqlite"
	projection "github.com/baldaworks/boyan/internal/projection"
	m "github.com/baldaworks/boyan/pkg/memory"
)

func openLargeForgetProjection(t *testing.T, path string, store coreOwner,
	reader coreReader,
) (*index.Store, *projection.ProjectionRuntime) {
	t.Helper()

	requireNoError(t, os.MkdirAll(path, 0o700), "private state: %v")
	idx, err := index.Open(t.Context(), path, index.Options{})
	requireNoError(t, err, "index: %v")
	t.Cleanup(func() { requireNoError(t, idx.Close(), "index cleanup: %v") })

	applier, err := index.NewSQLiteCanonicalApplier(idx, reader, store)
	requireNoError(t, err, "applier: %v")
	runtime, err := projection.NewProjectionRuntime(store, store, applier, "sqlite", 128)
	requireNoError(t, err, "runtime: %v")
	t.Cleanup(func() { requireNoError(t, runtime.Close(context.Background()), "runtime cleanup: %v") })

	return idx, runtime
}

func assertLargeForgetIndex(
	t *testing.T, idx *index.Store, scope m.Scope, minimum uint64, revision string,
) {
	t.Helper()

	hits, err := idx.SearchRecall(context.Background(), m.RecallRequest{
		Scope: scope, Query: "живу", Limit: 10, MinScopeChangeSeq: minimum,
	})
	requireNoError(t, err, "physical index freshness: %v")

	if len(hits) != 1 || revision != "" && hits[0].RevisionID != revision {
		t.Fatalf("active index retains stale forgotten candidates: %+v", hits)
	}
}
