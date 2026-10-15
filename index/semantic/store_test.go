package semantic_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	semanticstore "github.com/baldaworks/boyan/index/semantic"
	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

type stageMutation func(*semanticstore.Generation, []semanticstore.CoverageRecord, []semanticstore.Chunk)

//nolint:cyclop // One end-to-end lifecycle test keeps publication and snapshot assertions together.
func TestStagePublishAndSnapshotRequireExactCompatibleView(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := semanticstore.Open(filepath.Join(t.TempDir(), "embeddings"))
	requireNoError(t, err)
	t.Cleanup(func() { requireNoError(t, store.Close()) })

	generation, coverage, chunks := fixture("build-1", "bleve-1", 7, "a")
	state, err := store.Stage(ctx, generation, coverage, chunks)
	requireNoError(t, err)

	if state.Status != semanticstore.StatusReady || state.ChunkCount != 2 || state.RecordCount != 1 {
		t.Fatalf("Stage() state = %#v", state)
	}

	if _, found, snapshotErr := store.Snapshot(ctx, generation.View()); snapshotErr != nil || found {
		t.Fatalf("Snapshot(staged) = (_, %t, %v), want (_, false, nil)", found, snapshotErr)
	}

	err = store.Publish(ctx, semanticstore.Publication{
		View:                  generation.View(),
		BuildID:               generation.BuildID,
		ExpectedActiveBuildID: "",
	})
	requireNoError(t, err)

	snapshot, found, err := store.Snapshot(ctx, generation.View())
	requireNoError(t, err)

	if !found {
		t.Fatal("Snapshot() found = false, want true")
	}

	got := snapshot.Chunks()
	if len(got) != 2 || got[0].Ordinal != 0 || got[1].Ordinal != 1 || got[0].RevisionID != "revision-a" {
		t.Fatalf("Snapshot().Chunks() = %#v", got)
	}

	got[0].Vector[0] = 0
	if reread := snapshot.Chunks(); reread[0].Vector[0] != 1 {
		t.Fatalf("snapshot was mutable through returned chunks: %#v", reread[0].Vector)
	}

	wrong := generation.View()

	wrong.ModelSpace = hash("f")
	if _, compatible, snapshotErr := store.Snapshot(ctx, wrong); snapshotErr != nil || compatible {
		t.Fatalf("Snapshot(wrong space) = (_, %t, %v), want (_, false, nil)", compatible, snapshotErr)
	}

	wrong = generation.View()

	wrong.LexicalWatermark++
	if _, compatible, snapshotErr := store.Snapshot(ctx, wrong); snapshotErr != nil || compatible {
		t.Fatalf("Snapshot(wrong watermark) = (_, %t, %v), want (_, false, nil)", compatible, snapshotErr)
	}
}

func TestStageRejectsForeignMalformedAndIncompleteChunks(t *testing.T) {
	t.Parallel()

	tests := map[string]stageMutation{
		"foreign scope": func(_ *semanticstore.Generation, _ []semanticstore.CoverageRecord, chunks []semanticstore.Chunk) {
			chunks[0].Scope.Key = "test:foreign"
		},
		"wrong model space": func(
			_ *semanticstore.Generation, _ []semanticstore.CoverageRecord, chunks []semanticstore.Chunk,
		) {
			chunks[0].ModelSpace = hash("f")
		},
		"wrong dimensions": func(
			_ *semanticstore.Generation, _ []semanticstore.CoverageRecord, chunks []semanticstore.Chunk,
		) {
			chunks[0].Dimensions++
		},
		"wrong vector shape": func(
			_ *semanticstore.Generation, _ []semanticstore.CoverageRecord, chunks []semanticstore.Chunk,
		) {
			chunks[0].Vector = []float32{1}
		},
		"ordinal gap": func(_ *semanticstore.Generation, _ []semanticstore.CoverageRecord, chunks []semanticstore.Chunk) {
			chunks[1].Ordinal = 2
		},
		"duplicate ordinal": func(
			_ *semanticstore.Generation, _ []semanticstore.CoverageRecord, chunks []semanticstore.Chunk,
		) {
			chunks[1].Ordinal = 0
		},
		"missing coverage": func(
			_ *semanticstore.Generation, coverage []semanticstore.CoverageRecord, _ []semanticstore.Chunk,
		) {
			coverage[0].ChunkCount = 3
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store, err := semanticstore.Open(filepath.Join(t.TempDir(), "embeddings"))
			requireNoError(t, err)
			t.Cleanup(func() { requireNoError(t, store.Close()) })

			generation, coverage, chunks := fixture("build-1", "bleve-1", 7, "a")
			mutate(&generation, coverage, chunks)

			_, err = store.Stage(context.Background(), generation, coverage, chunks)
			if !errors.Is(err, semanticstore.ErrInvalid) {
				t.Fatalf("Stage() error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestDegradedMarkerIsExactAndNeverReadableAsReady(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := semanticstore.Open(filepath.Join(t.TempDir(), "embeddings"))
	requireNoError(t, err)
	t.Cleanup(func() { requireNoError(t, store.Close()) })

	generation, _, _ := fixture("degraded-1", "bleve-1", 7, "a")

	state, err := store.PublishDegraded(ctx, semanticstore.Degraded{
		Generation: generation,
		SafeReason: semanticcore.ReasonProjectionCapacity,
	}, "")
	requireNoError(t, err)

	if state.Status != semanticstore.StatusDegraded || state.SafeReason != string(semanticcore.ReasonProjectionCapacity) {
		t.Fatalf("PublishDegraded() = %#v", state)
	}

	active, found, err := store.ActiveState(ctx, generation.Scope)
	requireNoError(t, err)

	if !found || active != state {
		t.Fatalf("ActiveState() = (%#v, %t), want (%#v, true)", active, found, state)
	}

	if _, compatible, err := store.Snapshot(ctx, generation.View()); err != nil || compatible {
		t.Fatalf("Snapshot(degraded) = (_, %t, %v), want (_, false, nil)", compatible, err)
	}
}

func TestReopenScrubsUnpublishedGenerationAndPreservesPublishedState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	directory := filepath.Join(t.TempDir(), "embeddings")
	store, err := semanticstore.Open(directory)
	requireNoError(t, err)

	active, activeCoverage, activeChunks := fixture("build-active", "bleve-1", 7, "a")
	orphan, orphanCoverage, orphanChunks := fixture("build-orphan", "bleve-2", 8, "b")
	activeState, err := store.Stage(ctx, active, activeCoverage, activeChunks)
	requireNoError(t, err)
	requireNoError(t, store.Publish(ctx, semanticstore.Publication{View: active.View(), BuildID: active.BuildID}))
	_, err = store.Stage(ctx, orphan, orphanCoverage, orphanChunks)
	requireNoError(t, err)
	requireNoError(t, store.Close())

	reopened, err := semanticstore.Open(directory)
	requireNoError(t, err)
	t.Cleanup(func() { requireNoError(t, reopened.Close()) })

	if _, found, err := reopened.Snapshot(ctx, active.View()); err != nil || !found {
		t.Fatalf("Snapshot(active after reopen) = (_, %t, %v)", found, err)
	}

	if err := reopened.Publish(ctx, semanticstore.Publication{
		View: orphan.View(), BuildID: orphan.BuildID,
		ExpectedActiveBuildID: active.BuildID, ExpectedActiveEpoch: activeState.PublicationEpoch + 1,
	}); !errors.Is(err, semanticstore.ErrCorrupt) {
		t.Fatalf("Publish(scrubbed orphan) error = %v, want ErrCorrupt", err)
	}
}

func TestGarbageCollectionKeepsExistingSnapshotAndDropsOldGeneration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := semanticstore.Open(filepath.Join(t.TempDir(), "embeddings"))
	requireNoError(t, err)
	t.Cleanup(func() { requireNoError(t, store.Close()) })

	first, firstCoverage, firstChunks := fixture("build-1", "bleve-1", 7, "a")
	second, secondCoverage, secondChunks := fixture("build-2", "bleve-2", 8, "b")
	_, err = store.Stage(ctx, first, firstCoverage, firstChunks)
	requireNoError(t, err)
	requireNoError(t, store.Publish(ctx, semanticstore.Publication{View: first.View(), BuildID: first.BuildID}))
	oldSnapshot, _, err := store.Snapshot(ctx, first.View())
	requireNoError(t, err)
	_, err = store.Stage(ctx, second, secondCoverage, secondChunks)
	requireNoError(t, err)
	requireNoError(t, store.Publish(ctx, semanticstore.Publication{
		View: second.View(), BuildID: second.BuildID,
		ExpectedActiveBuildID: first.BuildID, ExpectedActiveEpoch: 1,
	}))
	requireNoError(t, store.CollectGarbage(ctx))

	if oldSnapshot.Chunks()[0].RevisionID != "revision-a" {
		t.Fatal("existing snapshot did not survive garbage collection")
	}

	err = store.Publish(ctx, semanticstore.Publication{
		View: first.View(), BuildID: first.BuildID,
		ExpectedActiveBuildID: second.BuildID, ExpectedActiveEpoch: 2,
	})
	if !errors.Is(err, semanticstore.ErrCorrupt) {
		t.Fatalf("Publish(collected old generation) = %v, want ErrCorrupt", err)
	}
}

func TestConcurrentSnapshotsNeverMixGenerations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := semanticstore.Open(filepath.Join(t.TempDir(), "embeddings"))
	requireNoError(t, err)
	t.Cleanup(func() { requireNoError(t, store.Close()) })

	first, firstCoverage, firstChunks := fixture("build-1", "bleve-1", 7, "a")
	second, secondCoverage, secondChunks := fixture("build-2", "bleve-2", 8, "b")
	_, err = store.Stage(ctx, first, firstCoverage, firstChunks)
	requireNoError(t, err)
	_, err = store.Stage(ctx, second, secondCoverage, secondChunks)
	requireNoError(t, err)
	requireNoError(t, store.Publish(ctx, semanticstore.Publication{View: first.View(), BuildID: first.BuildID}))

	start := make(chan struct{})
	errorsSeen := make(chan error, 8)

	var readers sync.WaitGroup
	for range 8 {
		readers.Add(1)
		go func() {
			defer readers.Done()

			<-start

			for range 50 {
				for _, candidate := range []semanticstore.Generation{first, second} {
					snapshot, found, err := store.Snapshot(ctx, candidate.View())
					if err != nil {
						errorsSeen <- err

						return
					}

					if found {
						chunks := snapshot.Chunks()
						if len(chunks) != 2 || chunks[0].BuildID != candidate.BuildID || chunks[1].BuildID != candidate.BuildID {
							errorsSeen <- errors.New("mixed semantic generations")

							return
						}
					}
				}
			}
		}()
	}

	close(start)
	requireNoError(t, store.Publish(ctx, semanticstore.Publication{
		View: second.View(), BuildID: second.BuildID,
		ExpectedActiveBuildID: first.BuildID, ExpectedActiveEpoch: 1,
	}))
	readers.Wait()
	close(errorsSeen)

	for err := range errorsSeen {
		t.Fatal(err)
	}
}

func TestDirectoryIsPrivateAndDisposableWithoutTouchingSiblings(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	canonicalMarker := filepath.Join(root, "canonical.keep")
	bleveMarker := filepath.Join(root, "bleve.keep")

	requireNoError(t, os.WriteFile(canonicalMarker, []byte("canonical"), 0o600))
	requireNoError(t, os.WriteFile(bleveMarker, []byte("bleve"), 0o600))

	directory := filepath.Join(root, "embeddings")
	store, err := semanticstore.Open(directory)
	requireNoError(t, err)
	info, err := os.Stat(directory)
	requireNoError(t, err)

	if info.Mode().Perm() != 0o700 {
		t.Fatalf("embeddings permissions = %#o, want 0700", info.Mode().Perm())
	}

	requireNoError(t, store.Close())
	requireNoError(t, os.RemoveAll(directory))

	if _, err := os.Stat(canonicalMarker); err != nil {
		t.Fatalf("canonical sibling affected by embeddings deletion: %v", err)
	}

	if _, err := os.Stat(bleveMarker); err != nil {
		t.Fatalf("Bleve sibling affected by embeddings deletion: %v", err)
	}

	reopened, err := semanticstore.Open(directory)
	requireNoError(t, err)
	requireNoError(t, reopened.Close())
}

func TestPublishSwitchIsAtomicAndUsesActivePointerCAS(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := semanticstore.Open(filepath.Join(t.TempDir(), "embeddings"))
	requireNoError(t, err)
	t.Cleanup(func() { requireNoError(t, store.Close()) })

	first, firstCoverage, firstChunks := fixture("build-1", "bleve-1", 7, "a")
	second, secondCoverage, secondChunks := fixture("build-2", "bleve-2", 8, "b")
	_, err = store.Stage(ctx, first, firstCoverage, firstChunks)
	requireNoError(t, err)
	_, err = store.Stage(ctx, second, secondCoverage, secondChunks)
	requireNoError(t, err)
	requireNoError(t, store.Publish(ctx, semanticstore.Publication{View: first.View(), BuildID: first.BuildID}))

	err = store.Publish(ctx, semanticstore.Publication{
		View:                  second.View(),
		BuildID:               second.BuildID,
		ExpectedActiveBuildID: "not-the-active-build",
		ExpectedActiveEpoch:   1,
	})
	if !errors.Is(err, semanticstore.ErrConflict) {
		t.Fatalf("Publish(stale CAS) error = %v, want ErrConflict", err)
	}

	oldSnapshot, found, err := store.Snapshot(ctx, first.View())
	requireNoError(t, err)

	if !found {
		t.Fatal("old Snapshot() found = false")
	}

	requireNoError(t, store.Publish(ctx, semanticstore.Publication{
		View:                  second.View(),
		BuildID:               second.BuildID,
		ExpectedActiveBuildID: first.BuildID,
		ExpectedActiveEpoch:   1,
	}))

	if old := oldSnapshot.Chunks(); len(old) != 2 || old[0].RevisionID != "revision-a" {
		t.Fatalf("old snapshot changed after publication: %#v", old)
	}

	if _, found, err := store.Snapshot(ctx, first.View()); err != nil || found {
		t.Fatalf("Snapshot(old view after switch) = (_, %t, %v)", found, err)
	}

	newSnapshot, found, err := store.Snapshot(ctx, second.View())
	requireNoError(t, err)

	if !found || newSnapshot.Chunks()[0].RevisionID != "revision-b" {
		t.Fatalf("Snapshot(new view) = (%#v, %t)", newSnapshot, found)
	}
}

func TestProjectionBoundsAcceptExactLimitsAndRejectOneOver(t *testing.T) {
	t.Parallel()

	requireNoError(t, semanticstore.CheckProjectionBounds(
		semanticcore.ProjectionChunkLimit,
		semanticcore.ProjectionByteLimit,
	))

	err := semanticstore.CheckProjectionBounds(
		semanticcore.ProjectionChunkLimit+1,
		semanticcore.ProjectionByteLimit,
	)
	if !errors.Is(err, semanticstore.ErrCapacity) {
		t.Fatalf("CheckProjectionBounds(chunks over) = %v, want ErrCapacity", err)
	}

	err = semanticstore.CheckProjectionBounds(
		semanticcore.ProjectionChunkLimit,
		semanticcore.ProjectionByteLimit+1,
	)
	if !errors.Is(err, semanticstore.ErrCapacity) {
		t.Fatalf("CheckProjectionBounds(bytes over) = %v, want ErrCapacity", err)
	}
}

func TestStageAcceptsExactly8192ChunksAndRejects8193(t *testing.T) {
	ctx := context.Background()
	store, err := semanticstore.Open(filepath.Join(t.TempDir(), "embeddings"))
	requireNoError(t, err)
	t.Cleanup(func() { requireNoError(t, store.Close()) })

	generation, coverage, _ := fixture("max-build", "bleve-max", 1, "max")
	generation.Dimensions = 1
	coverage[0].ChunkCount = semanticcore.ProjectionChunkLimit

	chunks := make([]semanticstore.Chunk, semanticcore.ProjectionChunkLimit)
	for index := range chunks {
		chunks[index] = chunk(generation, coverage[0], uint32(index))
		chunks[index].Vector = []float32{1}
		chunks[index].InputHash = hash("3")
	}

	state, err := store.Stage(ctx, generation, coverage, chunks)
	requireNoError(t, err)

	if state.ChunkCount != semanticcore.ProjectionChunkLimit {
		t.Fatalf("Stage(exact chunk cap) count = %d", state.ChunkCount)
	}

	overGeneration, overCoverage, _ := fixture("over-build", "bleve-over", 2, "over")
	overGeneration.Dimensions = 1
	overCoverage[0].ChunkCount = semanticcore.ProjectionChunkLimit + 1

	overChunks := make([]semanticstore.Chunk, semanticcore.ProjectionChunkLimit+1)
	for index := range overChunks {
		overChunks[index] = chunk(overGeneration, overCoverage[0], uint32(index))
		overChunks[index].Vector = []float32{1}
		overChunks[index].InputHash = hash("3")
	}

	if _, err := store.Stage(ctx, overGeneration, overCoverage, overChunks); !errors.Is(err, semanticstore.ErrCapacity) {
		t.Fatalf("Stage(one over chunk cap) error = %v, want ErrCapacity", err)
	}
}

func fixture(
	buildID string,
	lexicalGeneration string,
	watermark uint64,
	suffix string,
) (semanticstore.Generation, []semanticstore.CoverageRecord, []semanticstore.Chunk) {
	scope := memory.Scope{Key: "test:scope", Kind: memory.ScopeKindPersonal}
	space := hash("1")
	contentHash := hash("2")
	generation := semanticstore.Generation{
		Scope:               scope,
		BuildID:             buildID,
		LexicalGenerationID: lexicalGeneration,
		LexicalWatermark:    watermark,
		ModelSpace:          space,
		Dimensions:          2,
		ReadyAt:             time.Date(2026, time.October, 15, 1, 2, 3, 0, time.UTC),
	}
	coverage := []semanticstore.CoverageRecord{{
		ItemID:      "item-" + suffix,
		RevisionID:  "revision-" + suffix,
		Revision:    1,
		ContentHash: contentHash,
		ChunkCount:  2,
	}}
	chunks := []semanticstore.Chunk{
		chunk(generation, coverage[0], 0),
		chunk(generation, coverage[0], 1),
	}

	return generation, coverage, chunks
}

func chunk(
	generation semanticstore.Generation,
	coverage semanticstore.CoverageRecord,
	ordinal uint32,
) semanticstore.Chunk {
	return semanticstore.Chunk{
		Scope:               generation.Scope,
		BuildID:             generation.BuildID,
		LexicalGenerationID: generation.LexicalGenerationID,
		LexicalWatermark:    generation.LexicalWatermark,
		ModelSpace:          generation.ModelSpace,
		Dimensions:          generation.Dimensions,
		ItemID:              coverage.ItemID,
		RevisionID:          coverage.RevisionID,
		Revision:            coverage.Revision,
		Ordinal:             ordinal,
		ContentHash:         coverage.ContentHash,
		InputHash:           hash(string(rune('3' + ordinal))),
		ScopeChangeSeq:      generation.LexicalWatermark,
		Kind:                memory.MemoryKindState,
		CreatedAt:           time.Date(2026, time.October, 14, 1, 2, 3, 0, time.UTC),
		Vector:              []float32{1, 0},
	}
}

func hash(character string) string {
	result := ""
	for len(result) < 64 {
		result += character
	}

	return result[:64]
}

func requireNoError(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
