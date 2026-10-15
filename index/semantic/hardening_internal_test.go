package semantic

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

type cancelAfterContext struct {
	context.Context

	remaining atomic.Int64
	canceled  atomic.Bool
}

func newCancelAfterContext(checks int64) *cancelAfterContext {
	ctx := &cancelAfterContext{Context: context.Background()}
	ctx.remaining.Store(checks)

	return ctx
}

func (ctx *cancelAfterContext) Err() error {
	if ctx.canceled.Load() {
		return context.Canceled
	}

	if ctx.remaining.Add(-1) <= 0 {
		ctx.canceled.Store(true)

		return context.Canceled
	}

	return nil
}

func TestPublishDegradedChecksCancellationAfterLockBeforeTransaction(t *testing.T) {
	t.Parallel()

	store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

	generation, _, _ := internalFixture("cancel-degraded", "bleve-1", 1)

	ctx := newCancelAfterContext(4)

	_, err = store.PublishDegraded(ctx, Degraded{
		Generation: generation, SafeReason: semanticcore.ReasonProjectionCapacity,
	}, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PublishDegraded() error = %v, want context.Canceled", err)
	}

	if _, found, stateErr := store.ActiveState(context.Background(), generation.Scope); stateErr != nil || found {
		t.Fatalf("ActiveState() after canceled degraded publish = (_, %t, %v), want (_, false, nil)", found, stateErr)
	}
}

func TestStageChecksCancellationAfterLockBeforeClaim(t *testing.T) {
	t.Parallel()

	store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

	generation, coverage, chunks := internalFixture("cancel-after-lock", "bleve-1", 1)

	if _, err := store.Stage(newCancelAfterContext(12), generation, coverage, chunks); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stage() error = %v, want context.Canceled", err)
	}

	internalRequireNoError(t, store.db.View(func(transaction *badger.Txn) error {
		for _, key := range [][]byte{stateKey(generation.Scope, generation.BuildID)} {
			_, getErr := transaction.Get(key)
			if getErr != badger.ErrKeyNotFound {
				t.Fatalf("Stage persisted a generation claim after cancellation: %v", getErr)
			}
		}

		return nil
	}))
}

func TestCancellationCadenceDuringStageSortSnapshotScanAndGarbageScan(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

	generation, coverage, chunks := internalManyChunkFixture("cadence-stage", "bleve-1", 1, 100)
	if _, err := store.Stage(newCancelAfterContext(220), generation, coverage, chunks); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stage(canceled during sort) error = %v, want context.Canceled", err)
	}

	internalRequireNoError(t, store.db.View(func(transaction *badger.Txn) error {
		_, getErr := transaction.Get(stateKey(generation.Scope, generation.BuildID))
		if getErr != badger.ErrKeyNotFound {
			t.Fatalf("canceled Stage persisted state: %v", getErr)
		}

		return nil
	}))

	ready, readyCoverage, readyChunks := internalManyChunkFixture("cadence-ready", "bleve-2", 2, 100)
	_, err = store.Stage(context.Background(), ready, readyCoverage, readyChunks)
	internalRequireNoError(t, err)
	internalRequireNoError(t, store.Publish(context.Background(), Publication{View: ready.View(), BuildID: ready.BuildID}))

	if _, _, err := store.Snapshot(newCancelAfterContext(8), ready.View()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Snapshot(canceled during scan) error = %v, want context.Canceled", err)
	}

	internalRequireNoError(t, store.db.Update(func(transaction *badger.Txn) error {
		for index := 0; index < 100; index++ {
			key := stateKey(ready.Scope, fmt.Sprintf("gc-cadence-%03d", index))
			if setErr := transaction.Set(key, []byte("{}")); setErr != nil {
				return setErr
			}
		}

		return nil
	}))

	if err := store.CollectGarbage(newCancelAfterContext(10)); !errors.Is(err, context.Canceled) {
		t.Fatalf("CollectGarbage(canceled during scan) error = %v, want context.Canceled", err)
	}
}

func TestSnapshotChunksDeepCopyEveryReferenceField(t *testing.T) {
	t.Parallel()

	instant := time.Date(2026, time.October, 15, 3, 4, 5, 0, time.UTC)
	expected := instant
	snapshot := &Snapshot{chunks: []Chunk{{
		ValidFrom:       &instant,
		ValidUntil:      &instant,
		ExpiresAt:       &instant,
		SourceIDs:       []string{"source"},
		ConversationIDs: []string{"conversation"},
		Vector:          []float32{1},
	}}}

	got := snapshot.Chunks()
	mutated := instant.Add(time.Hour)
	*got[0].ValidFrom = mutated
	*got[0].ValidUntil = mutated
	*got[0].ExpiresAt = mutated
	got[0].SourceIDs[0] = "changed-source"
	got[0].ConversationIDs[0] = "changed-conversation"
	got[0].Vector[0] = 0

	reread := snapshot.Chunks()[0]
	if !reread.ValidFrom.Equal(expected) || !reread.ValidUntil.Equal(expected) || !reread.ExpiresAt.Equal(expected) ||
		reread.SourceIDs[0] != "source" || reread.ConversationIDs[0] != "conversation" || reread.Vector[0] != 1 {
		t.Fatalf("Snapshot.Chunks() leaked a reference: %#v", reread)
	}
}

func TestOperationsObserveCancellationWhileWaitingForStoreLock(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

	generation, coverage, chunks := internalFixture("blocked", "bleve-1", 1)
	tests := map[string]func(context.Context) error{
		"snapshot": func(ctx context.Context) error {
			_, _, callErr := store.Snapshot(ctx, generation.View())

			return callErr
		},
		"degraded": func(ctx context.Context) error {
			_, callErr := store.PublishDegraded(ctx, Degraded{
				Generation: generation, SafeReason: semanticcore.ReasonProjectionCapacity,
			}, "")

			return callErr
		},
		"stage": func(ctx context.Context) error {
			_, callErr := store.Stage(ctx, generation, coverage, chunks)

			return callErr
		},
		"garbage collection": store.CollectGarbage,
	}

	for name, operation := range tests {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())

			release, acquireErr := store.gate.acquire(context.Background(), true)
			internalRequireNoError(t, acquireErr)

			result := make(chan error, 1)
			go func() { result <- operation(ctx) }()

			cancel()

			select {
			case callErr := <-result:
				release()

				if !errors.Is(callErr, context.Canceled) {
					t.Fatalf("operation error = %v, want context.Canceled", callErr)
				}
			case <-time.After(250 * time.Millisecond):
				release()

				callErr := <-result
				t.Fatalf("operation ignored cancellation while blocked on lock; eventual error = %v", callErr)
			}
		})
	}
}

func TestGarbageCollectionScansAndDeletesManyOrphansInBoundedBatches(t *testing.T) {
	t.Parallel()

	store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

	const orphanGenerations = garbageDeleteBatch*2 + 37

	scope := memory.Scope{Key: "test:scope", Kind: memory.ScopeKindPersonal}

	for start := 0; start < orphanGenerations; start += garbageDeleteBatch {
		end := min(start+garbageDeleteBatch, orphanGenerations)

		internalRequireNoError(t, store.db.Update(func(transaction *badger.Txn) error {
			for index := start; index < end; index++ {
				buildID := fmt.Sprintf("orphan-%04d", index)
				if setErr := transaction.Set(stateKey(scope, buildID), []byte("{}")); setErr != nil {
					return setErr
				}

				if setErr := transaction.Set(chunkKey(scope, buildID, 0), []byte("{}")); setErr != nil {
					return setErr
				}
			}

			return nil
		}))
	}

	scanBatches := 0
	deleteBatches := 0
	store.afterGarbageScanBatch = func(size int) {
		scanBatches++

		if size > garbageDeleteBatch {
			t.Fatalf("scan batch size = %d, want <= %d", size, garbageDeleteBatch)
		}
	}
	store.afterGarbageDeleteBatch = func(size int) {
		deleteBatches++

		if size > garbageDeleteBatch {
			t.Fatalf("delete batch size = %d, want <= %d", size, garbageDeleteBatch)
		}
	}

	internalRequireNoError(t, store.CollectGarbage(context.Background()))

	if scanBatches < 6 || deleteBatches < 6 {
		t.Fatalf("bounded batches = scans %d, deletes %d; want at least 6 each", scanBatches, deleteBatches)
	}
}

func TestStageRejectsSiblingChunksWithDifferentRecordMetadata(t *testing.T) {
	t.Parallel()

	mutations := map[string]func(*Chunk){
		"scope sequence":   func(chunk *Chunk) { chunk.ScopeChangeSeq-- },
		"kind":             func(chunk *Chunk) { chunk.Kind = memory.MemoryKindEvent },
		"category":         func(chunk *Chunk) { chunk.Category = memory.AtomCategory("changed") },
		"memory key":       func(chunk *Chunk) { chunk.MemoryKey = memory.MemoryKey("changed") },
		"created at":       func(chunk *Chunk) { chunk.CreatedAt = chunk.CreatedAt.Add(time.Minute) },
		"valid from":       func(chunk *Chunk) { changed := chunk.ValidFrom.Add(time.Minute); chunk.ValidFrom = &changed },
		"valid until":      func(chunk *Chunk) { changed := chunk.ValidUntil.Add(time.Minute); chunk.ValidUntil = &changed },
		"expires at":       func(chunk *Chunk) { changed := chunk.ExpiresAt.Add(time.Minute); chunk.ExpiresAt = &changed },
		"source ids":       func(chunk *Chunk) { chunk.SourceIDs = []string{"different-source"} },
		"conversation ids": func(chunk *Chunk) { chunk.ConversationIDs = []string{"different-conversation"} },
	}

	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
			internalRequireNoError(t, err)
			t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

			generation, coverage, chunks := internalFixture("siblings", "bleve-1", 2)
			validFrom := generation.ReadyAt.Add(-time.Hour)
			validUntil := generation.ReadyAt.Add(time.Hour)
			expiresAt := generation.ReadyAt.Add(2 * time.Hour)
			chunks[0].Category = memory.AtomCategory("category")
			chunks[0].MemoryKey = memory.MemoryKey("memory-key")
			chunks[0].ValidFrom = &validFrom
			chunks[0].ValidUntil = &validUntil
			chunks[0].ExpiresAt = &expiresAt
			chunks[0].SourceIDs = []string{"source"}
			chunks[0].ConversationIDs = []string{"conversation"}
			chunks = append(chunks, cloneChunks(chunks)[0])
			chunks[1].Ordinal = 1
			chunks[1].InputHash = internalHash("d")
			coverage[0].ChunkCount = 2

			mutate(&chunks[1])

			if _, err := store.Stage(context.Background(), generation, coverage, chunks); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Stage(mismatched sibling metadata) error = %v, want ErrInvalid", err)
			}
		})
	}
}

//nolint:funlen // Exact preflight, stage, publication, and reopen checks form one byte-bound contract.
func TestExactEncodedProjectionLimitStagesPublishesAndReopensWhileOneOverRejects(t *testing.T) {
	generation, coverage, chunks := exactProjectionLimitFixture(t)

	missingVectors := append([]Chunk(nil), chunks...)
	for index := range missingVectors {
		missingVectors[index].Vector = nil
	}

	preflight, err := PreflightStage(context.Background(), generation, coverage, missingVectors)
	internalRequireNoError(t, err)

	if preflight.EncodedBytes != uint64(semanticcore.ProjectionByteLimit) {
		t.Fatalf("PreflightStage() EncodedBytes = %d, want %d", preflight.EncodedBytes, semanticcore.ProjectionByteLimit)
	}

	overDirectory := filepath.Join(t.TempDir(), "over")
	overStore, err := Open(overDirectory)
	internalRequireNoError(t, err)

	overChunks := append([]Chunk(nil), chunks...)
	overChunks[len(overChunks)-1].SourceIDs = append([]string(nil), chunks[len(chunks)-1].SourceIDs...)

	overChunks[len(overChunks)-1].SourceIDs[len(overChunks[len(overChunks)-1].SourceIDs)-1] += "x"

	overMissingVectors := append([]Chunk(nil), overChunks...)
	for index := range overMissingVectors {
		overMissingVectors[index].Vector = nil
	}

	if _, err := PreflightStage(
		context.Background(), generation, coverage, overMissingVectors,
	); !errors.Is(err, ErrCapacity) {
		t.Fatalf("PreflightStage(64 MiB + 1) error = %v, want ErrCapacity", err)
	}

	if _, err := overStore.Stage(context.Background(), generation, coverage, overChunks); !errors.Is(err, ErrCapacity) {
		t.Fatalf("Stage(64 MiB + 1) error = %v, want ErrCapacity", err)
	}

	internalRequireNoError(t, overStore.Close())

	directory := filepath.Join(t.TempDir(), "exact")
	store, err := Open(directory)
	internalRequireNoError(t, err)
	state, err := store.Stage(context.Background(), generation, coverage, chunks)
	internalRequireNoError(t, err)

	if state.EncodedBytes != uint64(semanticcore.ProjectionByteLimit) {
		t.Fatalf("Stage() EncodedBytes = %d, want %d", state.EncodedBytes, semanticcore.ProjectionByteLimit)
	}

	internalRequireNoError(t, store.Publish(context.Background(), Publication{
		View: generation.View(), BuildID: generation.BuildID,
	}))
	internalRequireNoError(t, store.Close())

	reopened, err := Open(directory)

	internalRequireNoError(t, err)
	defer func() { internalRequireNoError(t, reopened.Close()) }()

	reopenedState, found, err := reopened.ActiveState(context.Background(), generation.Scope)
	internalRequireNoError(t, err)

	if !found || reopenedState.EncodedBytes != uint64(semanticcore.ProjectionByteLimit) {
		t.Fatalf("ActiveState(reopened) = (%#v, %t), want exact limit", reopenedState, found)
	}
}

//nolint:cyclop,funlen // The table intentionally exercises each independent corrupt-record shape.
func TestSnapshotRejectsOversizedAndUnboundedCorruptRecords(t *testing.T) {
	t.Parallel()

	type corruption func(*badger.Txn, Generation) error

	corruptState := func(change func(*storedState)) corruption {
		return func(transaction *badger.Txn, generation Generation) error {
			item, err := transaction.Get(stateKey(generation.Scope, generation.BuildID))
			if err != nil {
				return err
			}

			value, err := readItemValue(item)
			if err != nil {
				return err
			}

			var record storedState
			if err := decodeJSON(value, &record); err != nil {
				return err
			}

			change(&record)

			value, err = encodeJSON(record)
			if err != nil {
				return err
			}

			return transaction.Set(stateKey(generation.Scope, generation.BuildID), value)
		}
	}
	corruptChunk := func(change func(*storedChunk)) corruption {
		return func(transaction *badger.Txn, generation Generation) error {
			key := chunkKey(generation.Scope, generation.BuildID, 0)

			item, err := transaction.Get(key)
			if err != nil {
				return err
			}

			value, err := readItemValue(item)
			if err != nil {
				return err
			}

			var record storedChunk
			if err := decodeJSON(value, &record); err != nil {
				return err
			}

			change(&record)

			value, err = encodeJSON(record)
			if err != nil {
				return err
			}

			return transaction.Set(key, value)
		}
	}

	tests := map[string]corruption{
		"huge chunk count": corruptState(func(record *storedState) {
			record.State.ChunkCount = ^uint32(0)
		}),
		"huge record count": corruptState(func(record *storedState) {
			record.State.RecordCount = ^uint32(0)
		}),
		"huge encoded bytes": corruptState(func(record *storedState) {
			record.State.EncodedBytes = ^uint64(0)
		}),
		"oversized state value": func(transaction *badger.Txn, generation Generation) error {
			return transaction.Set(
				stateKey(generation.Scope, generation.BuildID),
				bytes.Repeat([]byte("x"), maxStateValueBytes+1),
			)
		},
		"oversized chunk value": func(transaction *badger.Txn, generation Generation) error {
			return transaction.Set(
				chunkKey(generation.Scope, generation.BuildID, 0),
				bytes.Repeat([]byte("x"), maxChunkValueBytes+1),
			)
		},
		"huge vector": corruptChunk(func(record *storedChunk) {
			record.Vector = make([]byte, (semanticcore.MaxEmbeddingDimensions+1)*float32Bytes)
		}),
		"huge metadata count": corruptChunk(func(record *storedChunk) {
			record.SourceIDs = make([]string, maxMetadataIDs+1)
		}),
		"huge metadata field": corruptChunk(func(record *storedChunk) {
			record.SourceIDs = []string{strings.Repeat("x", maxIdentityBytes+1)}
		}),
	}

	for name, corrupt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
			internalRequireNoError(t, err)
			t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

			generation, coverage, chunks := internalFixture("corrupt", "bleve-1", 1)
			_, err = store.Stage(context.Background(), generation, coverage, chunks)
			internalRequireNoError(t, err)
			internalRequireNoError(t, store.Publish(context.Background(), Publication{
				View: generation.View(), BuildID: generation.BuildID,
			}))
			internalRequireNoError(t, store.db.Update(func(transaction *badger.Txn) error {
				return corrupt(transaction, generation)
			}))

			if _, _, err := store.Snapshot(context.Background(), generation.View()); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("Snapshot(corrupt record) error = %v, want ErrCorrupt", err)
			}
		})
	}
}

func TestConcurrentCloseIsIdempotentAndDatabaseFilesArePrivate(t *testing.T) {
	t.Parallel()

	directory := filepath.Join(t.TempDir(), "embeddings")
	store, err := Open(directory)
	internalRequireNoError(t, err)

	generation, coverage, chunks := internalFixture("permissions", "bleve-1", 1)
	_, err = store.Stage(context.Background(), generation, coverage, chunks)
	internalRequireNoError(t, err)

	const closers = 16

	errorsSeen := make(chan error, closers)

	var wait sync.WaitGroup
	for range closers {
		wait.Add(1)
		go func() {
			defer wait.Done()

			errorsSeen <- store.Close()
		}()
	}

	wait.Wait()
	close(errorsSeen)

	for closeErr := range errorsSeen {
		internalRequireNoError(t, closeErr)
	}

	internalRequireNoError(t, filepath.WalkDir(directory, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}

		if entry.IsDir() && info.Mode().Perm() != 0o700 {
			t.Fatalf("directory %q permissions = %#o, want 0700", path, info.Mode().Perm())
		}

		if info.Mode().IsRegular() && info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("file %q permissions = %#o, want no group/other access", path, info.Mode().Perm())
		}

		return nil
	}))

	if _, _, err := store.Snapshot(context.Background(), generation.View()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Snapshot(after Close) error = %v, want ErrClosed", err)
	}
}

func TestStartupCleanupRemovesManyInterruptedGenerationsInBoundedPasses(t *testing.T) {
	t.Parallel()

	directory := filepath.Join(t.TempDir(), "embeddings")
	store, err := Open(directory)
	internalRequireNoError(t, err)

	scope := memory.Scope{Key: "test:startup-orphans", Kind: memory.ScopeKindPersonal}

	const orphanGenerations = garbageDeleteBatch*2 + 19
	for start := 0; start < orphanGenerations; start += garbageDeleteBatch {
		end := min(start+garbageDeleteBatch, orphanGenerations)

		internalRequireNoError(t, store.db.Update(func(transaction *badger.Txn) error {
			for index := start; index < end; index++ {
				buildID := fmt.Sprintf("interrupted-%04d", index)
				if setErr := transaction.Set(legacyUsedBuildKey(scope, buildID), []byte{keyVersion}); setErr != nil {
					return setErr
				}

				if setErr := transaction.Set(stateKey(scope, buildID), []byte("{}")); setErr != nil {
					return setErr
				}

				if setErr := transaction.Set(chunkKey(scope, buildID, 0), []byte("{}")); setErr != nil {
					return setErr
				}
			}

			return nil
		}))
	}

	internalRequireNoError(t, store.Close())

	reopened, err := Open(directory)

	internalRequireNoError(t, err)
	defer func() { internalRequireNoError(t, reopened.Close()) }()

	internalRequireNoError(t, reopened.db.View(func(transaction *badger.Txn) error {
		for _, prefix := range [][]byte{statePrefix(), allChunkPrefix()} {
			iterator := transaction.NewIterator(badger.DefaultIteratorOptions)
			iterator.Seek(prefix)
			found := iterator.ValidForPrefix(prefix)
			iterator.Close()

			if found {
				t.Fatalf("startup cleanup left keys under prefix %x", prefix)
			}
		}

		return nil
	}))
}

//nolint:funlen // Keeping exact byte construction together makes the 64 MiB proof auditable.
func exactProjectionLimitFixture(t *testing.T) (Generation, []CoverageRecord, []Chunk) {
	t.Helper()

	scope := memory.Scope{Key: "test:size-scope", Kind: memory.ScopeKindPersonal}
	generation := Generation{
		Scope: scope, BuildID: "size64-a", LexicalGenerationID: "bleve-64",
		LexicalWatermark: 1, ModelSpace: internalHash("a"), Dimensions: semanticcore.MaxEmbeddingDimensions,
		ReadyAt: time.Date(2026, time.October, 15, 1, 2, 3, 0, time.UTC),
	}
	vector := make([]float32, generation.Dimensions)
	vector[0] = 1
	coverage := make([]CoverageRecord, 0, 4096)
	chunks := make([]Chunk, 0, 4096)
	rowBytes := uint64(0)
	limit := uint64(semanticcore.ProjectionByteLimit)

	for index := 0; ; index++ {
		identity := fmt.Sprintf("record-%04d", index)
		manifest := CoverageRecord{
			ItemID: identity, RevisionID: identity, Revision: 1, ContentHash: internalHash("b"), ChunkCount: 1,
		}
		chunk := Chunk{
			Scope: scope, BuildID: generation.BuildID, LexicalGenerationID: generation.LexicalGenerationID,
			LexicalWatermark: generation.LexicalWatermark, ModelSpace: generation.ModelSpace,
			Dimensions: generation.Dimensions, ItemID: identity, RevisionID: identity, Revision: 1,
			ContentHash: manifest.ContentHash, InputHash: internalHash("c"), ScopeChangeSeq: 1,
			Kind: memory.MemoryKindState, CreatedAt: generation.ReadyAt, Vector: vector,
		}
		encoded, err := encodeChunk(chunk)
		internalRequireNoError(t, err)

		candidateRowBytes := rowBytes + uint64(len(chunkKey(scope, generation.BuildID, uint32(index)))+len(encoded))

		candidateCoverage := append(coverage, manifest)

		candidateBytes := uncheckedProjectionBytes(t, generation, candidateCoverage, candidateRowBytes)
		if candidateBytes > limit {
			break
		}

		coverage = candidateCoverage

		chunks = append(chunks, chunk)
		rowBytes = candidateRowBytes
	}

	baseBytes := uncheckedProjectionBytes(t, generation, coverage, rowBytes)

	deficit := int(limit - baseBytes)
	if deficit < 32 {
		last := len(chunks) - 1
		encoded, err := encodeChunk(chunks[last])
		internalRequireNoError(t, err)

		rowBytes -= uint64(len(chunkKey(scope, generation.BuildID, uint32(last))) + len(encoded))
		chunks = chunks[:last]
		coverage = coverage[:last]
		baseBytes = uncheckedProjectionBytes(t, generation, coverage, rowBytes)
		deficit = int(limit - baseBytes)
	}

	last := len(chunks) - 1
	baseEncoded, err := encodeChunk(chunks[last])
	internalRequireNoError(t, err)
	chunks[last].SourceIDs = metadataPaddingForEncodedDelta(t, chunks[last], len(baseEncoded), deficit)
	paddedEncoded, err := encodeChunk(chunks[last])
	internalRequireNoError(t, err)

	rowBytes += uint64(len(paddedEncoded) - len(baseEncoded))
	if actual := uncheckedProjectionBytes(t, generation, coverage, rowBytes); actual != limit {
		t.Fatalf("exact fixture bytes = %d, want %d", actual, limit)
	}

	return generation, coverage, chunks
}

func metadataPaddingForEncodedDelta(t *testing.T, chunk Chunk, baseLength, target int) []string {
	t.Helper()

	for count := 1; count <= maxMetadataIDs; count++ {
		ids := make([]string, count)
		for index := range ids {
			ids[index] = fmt.Sprintf("%03d", index)
		}

		candidate := chunk
		candidate.SourceIDs = ids
		encoded, err := encodeChunk(candidate)
		internalRequireNoError(t, err)

		extra := target - (len(encoded) - baseLength)
		if extra < 0 || extra > count*(maxIdentityBytes-3) {
			continue
		}

		for index := range ids {
			addition := min(extra, maxIdentityBytes-len(ids[index]))
			ids[index] += strings.Repeat("x", addition)
			extra -= addition
		}

		candidate.SourceIDs = ids
		encoded, err = encodeChunk(candidate)
		internalRequireNoError(t, err)

		if len(encoded)-baseLength == target {
			return ids
		}
	}

	t.Fatalf("could not encode %d bytes of bounded metadata padding", target)

	return nil
}

func uncheckedProjectionBytes(
	t *testing.T,
	generation Generation,
	coverage []CoverageRecord,
	rowBytes uint64,
) uint64 {
	t.Helper()

	state := State{
		SchemaVersion: schemaVersion, Scope: generation.Scope, BuildID: generation.BuildID,
		LexicalGenerationID: generation.LexicalGenerationID, LexicalWatermark: generation.LexicalWatermark,
		ModelSpace: generation.ModelSpace, Dimensions: generation.Dimensions, Status: StatusReady,
		ChunkCount: uint32(len(coverage)), RecordCount: uint32(len(coverage)),
		CoverageHash: coverageFingerprint(coverage), Omitted: generation.Omitted, ReadyAt: generation.ReadyAt.UTC(),
	}
	for range encodedSizeConvergenceLimit {
		encoded, err := encodeJSON(storedState{SchemaVersion: schemaVersion, Complete: true, State: state})
		internalRequireNoError(t, err)

		actual := rowBytes + uint64(len(stateKey(state.Scope, state.BuildID))+len(encoded))
		if actual == state.EncodedBytes {
			return actual
		}

		state.EncodedBytes = actual
	}

	t.Fatal("projection size did not converge")

	return 0
}

func TestStagePreflightRejectsAmplifyingInputsBeforeCloneOrSort(t *testing.T) {
	t.Parallel()

	generation, coverage, chunks := internalFixture("bounded", "bleve-1", 1)

	tests := map[string]struct {
		coverage []CoverageRecord
		chunks   []Chunk
		want     error
	}{
		"coverage over cap": {
			coverage: make([]CoverageRecord, semanticcore.ProjectionChunkLimit+1),
			want:     ErrCapacity,
		},
		"coverage exceeds chunks": {
			coverage: append(append([]CoverageRecord(nil), coverage...), coverage[0]),
			chunks:   chunks,
			want:     ErrInvalid,
		},
		"nested metadata count": {
			coverage: coverage,
			chunks: func() []Chunk {
				candidate := append([]Chunk(nil), chunks...)
				candidate[0].SourceIDs = make([]string, maxMetadataIDs+1)

				return candidate
			}(),
			want: ErrInvalid,
		},
		"nested metadata field": {
			coverage: coverage,
			chunks: func() []Chunk {
				candidate := append([]Chunk(nil), chunks...)
				candidate[0].SourceIDs = []string{string(bytes.Repeat([]byte("x"), maxIdentityBytes+1))}

				return candidate
			}(),
			want: ErrInvalid,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := preflightStageInputs(context.Background(), generation, test.coverage, test.chunks)
			if !errors.Is(err, test.want) {
				t.Fatalf("preflightStageInputs() error = %v, want %v", err, test.want)
			}
		})
	}
}

func internalManyChunkFixture(buildID, lexicalGeneration string, watermark uint64, count int) (
	Generation,
	[]CoverageRecord,
	[]Chunk,
) {
	generation, coverage, chunks := internalFixture(buildID, lexicalGeneration, watermark)
	coverage[0].ChunkCount = uint32(count)

	chunks = append(make([]Chunk, 0, count), chunks[0])
	for index := 1; index < count; index++ {
		chunk := chunks[0]
		chunk.Ordinal = uint32(index)
		chunk.Vector = append([]float32(nil), chunk.Vector...)
		chunks = append(chunks, chunk)
	}

	return generation, coverage, chunks
}

func TestStateHeaderPreflightRejectsUnboundedAllocationInputs(t *testing.T) {
	t.Parallel()

	tests := map[string]State{
		"chunk count":   {ChunkCount: semanticcore.ProjectionChunkLimit + 1},
		"record count":  {ChunkCount: 1, RecordCount: 2},
		"encoded bytes": {EncodedBytes: semanticcore.ProjectionByteLimit + 1},
	}

	for name, state := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := preflightStoredState(state); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("preflightStoredState() error = %v, want ErrCorrupt", err)
			}
		})
	}
}

func TestBoundedItemReadRejectsOversizedValueBeforeCopy(t *testing.T) {
	t.Parallel()

	db, err := badger.Open(badger.DefaultOptions(filepath.Join(t.TempDir(), "badger")).WithLogger(nil))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, db.Close()) })

	value := bytes.Repeat([]byte("x"), maxChunkValueBytes+1)

	internalRequireNoError(t, db.Update(func(transaction *badger.Txn) error {
		return transaction.Set([]byte("oversized"), value)
	}))

	internalRequireNoError(t, db.View(func(transaction *badger.Txn) error {
		item, getErr := transaction.Get([]byte("oversized"))
		if getErr != nil {
			return getErr
		}

		_, readErr := readItemValueBounded(item, maxChunkValueBytes)
		if !errors.Is(readErr, ErrCorrupt) {
			t.Fatalf("readItemValueBounded() error = %v, want ErrCorrupt", readErr)
		}

		return nil
	}))
}
