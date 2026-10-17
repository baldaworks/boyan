package canonicalcore_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	index "github.com/baldaworks/boyan/index/sqlite"
	"github.com/baldaworks/boyan/internal/forgetops"
	projection "github.com/baldaworks/boyan/internal/projection"
	m "github.com/baldaworks/boyan/pkg/memory"
	b "github.com/baldaworks/boyan/store/badger"
)

func TestLargeForgetRebuildsLiveProjectionAndPreservesLaterTurn(t *testing.T) {
	reader, processor, turn, _, store := nativeFlow(t)
	ingestNativeHistory(t, processor, turn, 1024)
	path := filepath.Join(t.TempDir(), "index")
	open := func() (*index.Store, *projection.ProjectionRuntime) {
		return openLargeForgetProjection(t, path, store, reader)
	}
	idx, runtime := open()
	ctx := context.Background()
	requireNoError(t, runtime.Sync(ctx, turn.Scope), "initial sync: %v")
	assertLargeForgetIndex(t, idx, turn.Scope, 0, "")

	adapter, err := forgetops.NewCanonicalForgetAdapter(store)
	requireNoError(t, err, "adapter: %v")

	command := m.ForgetScopeCommand{SchemaVersion: m.DerivedSchemaVersionV1,
		Scope: turn.Scope, RequestID: "large-index-rebuild", ForgottenAt: time.Now().UTC()}
	outcome, err := adapter.ForgetScope(ctx, command)
	requireNoError(t, err, "large forget: %v")

	if !outcome.ReferencesTruncated {
		t.Fatal("fixture must exceed bounded outcome references")
	}

	next, err := m.NewTurn(turn.Scope, turn.Conversation, "after-large-forget", turn.CompletedAt.Add(2*time.Hour),
		"Я живу в Казани", "OK")
	requireNoError(t, err, "later turn: %v")
	later, err := processor.ProcessTurn(ctx, next, testProcessorDerivation())
	requireNoError(t, err, "later ingest: %v")
	requireNoError(t, runtime.Sync(ctx, turn.Scope), "bounded invalidation rebuild: %v")
	assertLargeForgetIndex(t, idx, turn.Scope, later.ChangeSeq, later.RevisionIDs[0])
	requireNoError(t, runtime.Close(ctx), "runtime close: %v")
	requireNoError(t, idx.Close(), "index close: %v")

	idx, runtime = open()
	defer func() { requireNoError(t, runtime.Close(ctx), "runtime close: %v") }()
	defer func() { requireNoError(t, idx.Close(), "index close: %v") }()

	requireNoError(t, runtime.Start(ctx), "restart: %v")
	replay, err := adapter.ForgetScope(ctx, command)
	requireNoError(t, err, "old erase replay: %v")

	if replay.ScopeVersion != outcome.ScopeVersion {
		t.Fatal("replay changed outcome")
	}

	requireNoError(t, runtime.Sync(ctx, turn.Scope), "replay sync: %v")
	assertLargeForgetIndex(t, idx, turn.Scope, later.ChangeSeq, later.RevisionIDs[0])
}

func openLargeForgetProjection(t *testing.T, path string, store *b.CanonicalStore,
	reader *b.CanonicalReader,
) (*index.Store, *projection.ProjectionRuntime) {
	t.Helper()

	requireNoError(t, os.MkdirAll(path, 0o700), "private state: %v")
	idx, err := index.Open(t.Context(), path, index.Options{})
	requireNoError(t, err, "index: %v")
	applier, err := index.NewSQLiteCanonicalApplier(idx, reader, store)
	requireNoError(t, err, "applier: %v")
	runtime, err := projection.NewProjectionRuntime(store, store, applier, "sqlite", 128)
	requireNoError(t, err, "runtime: %v")

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
