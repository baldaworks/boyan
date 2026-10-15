package semantic

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func TestCanceledStagingLeavesActivePointerAndStartupScrubsOrphan(t *testing.T) {
	t.Parallel()

	directory := filepath.Join(t.TempDir(), "embeddings")
	store, err := Open(directory)
	internalRequireNoError(t, err)

	active, coverage, chunks := internalFixture("active", "bleve-1", 1)
	_, err = store.Stage(context.Background(), active, coverage, chunks)
	internalRequireNoError(t, err)
	internalRequireNoError(t, store.Publish(context.Background(), Publication{
		View: active.View(), BuildID: active.BuildID,
	}))

	orphan, orphanCoverage, orphanChunks := internalFixture("orphan", "bleve-2", 2)
	ctx, cancel := context.WithCancel(context.Background())

	store.afterStageBatch = func(int) error {
		cancel()

		return nil
	}
	if _, err := store.Stage(ctx, orphan, orphanCoverage, orphanChunks); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stage(canceled) error = %v, want context.Canceled", err)
	}

	state, found, err := store.ActiveState(context.Background(), active.Scope)
	if err != nil || !found || state.BuildID != active.BuildID {
		t.Fatalf("ActiveState() after canceled stage = (%#v, %t, %v)", state, found, err)
	}

	store.afterStageBatch = nil
	internalRequireNoError(t, store.Close())

	reopened, err := Open(directory)
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, reopened.Close()) })

	if err := reopened.Publish(context.Background(), Publication{
		View: orphan.View(), BuildID: orphan.BuildID,
		ExpectedActiveBuildID: active.BuildID, ExpectedActiveEpoch: state.PublicationEpoch,
	}); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Publish(orphan after restart) error = %v, want ErrCorrupt", err)
	}
}

func TestInjectedStagingFailureNeverChangesActivePointer(t *testing.T) {
	t.Parallel()

	store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

	active, coverage, chunks := internalFixture("active", "bleve-1", 1)
	_, err = store.Stage(context.Background(), active, coverage, chunks)
	internalRequireNoError(t, err)
	internalRequireNoError(t, store.Publish(context.Background(), Publication{
		View: active.View(), BuildID: active.BuildID,
	}))

	injected := errors.New("injected stage failure")
	failed, failedCoverage, failedChunks := internalFixture("failed", "bleve-2", 2)

	store.afterStageBatch = func(int) error { return injected }
	if _, err := store.Stage(context.Background(), failed, failedCoverage, failedChunks); !errors.Is(err, injected) {
		t.Fatalf("Stage(injected failure) error = %v, want injected error", err)
	}

	state, found, err := store.ActiveState(context.Background(), active.Scope)
	internalRequireNoError(t, err)

	if !found || state.BuildID != active.BuildID {
		t.Fatalf("active pointer changed after staging failure: %#v, %t", state, found)
	}
}

func TestSnapshotRejectsCorruptChunkEncoding(t *testing.T) {
	t.Parallel()

	store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

	generation, coverage, chunks := internalFixture("active", "bleve-1", 1)
	_, err = store.Stage(context.Background(), generation, coverage, chunks)
	internalRequireNoError(t, err)
	internalRequireNoError(t, store.Publish(context.Background(), Publication{
		View: generation.View(), BuildID: generation.BuildID,
	}))

	internalRequireNoError(t, store.db.Update(func(transaction *badger.Txn) error {
		return transaction.Set(chunkKey(generation.Scope, generation.BuildID, 0), []byte("{"))
	}))

	if _, _, err := store.Snapshot(context.Background(), generation.View()); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Snapshot(corrupt chunk) error = %v, want ErrCorrupt", err)
	}
}

func TestOpenFailsClosedWhenActiveStateIsMissing(t *testing.T) {
	testOpenFailsClosedOnActiveCorruption(t, func(transaction *badger.Txn, generation Generation) error {
		if err := transaction.Delete(stateKey(generation.Scope, generation.BuildID)); err != nil {
			return err
		}

		return transaction.Delete(chunkKey(generation.Scope, generation.BuildID, 0))
	})
}

func TestOpenFailsClosedWhenActiveChunkIsMissing(t *testing.T) {
	testOpenFailsClosedOnActiveCorruption(t, func(transaction *badger.Txn, generation Generation) error {
		return transaction.Delete(chunkKey(generation.Scope, generation.BuildID, 0))
	})
}

func TestOpenFailsClosedWhenActiveChunkIsCorrupt(t *testing.T) {
	testOpenFailsClosedOnActiveCorruption(t, func(transaction *badger.Txn, generation Generation) error {
		return transaction.Set(chunkKey(generation.Scope, generation.BuildID, 0), []byte("{"))
	})
}

func testOpenFailsClosedOnActiveCorruption(
	t *testing.T,
	corrupt func(*badger.Txn, Generation) error,
) {
	t.Helper()

	directory := filepath.Join(t.TempDir(), "embeddings")
	store, err := Open(directory)
	internalRequireNoError(t, err)

	generation, coverage, chunks := internalFixture("startup-active", "bleve-1", 1)
	_, err = store.Stage(context.Background(), generation, coverage, chunks)
	internalRequireNoError(t, err)
	internalRequireNoError(t, store.Publish(context.Background(), Publication{
		View: generation.View(), BuildID: generation.BuildID,
	}))

	evidenceKey := legacyUsedBuildKey(generation.Scope, "startup-gc-evidence")

	internalRequireNoError(t, store.db.Update(func(transaction *badger.Txn) error {
		if err := transaction.Set(evidenceKey, []byte("evidence")); err != nil {
			return err
		}

		return corrupt(transaction, generation)
	}))
	internalRequireNoError(t, store.Close())

	reopened, err := Open(directory)
	if reopened != nil {
		_ = reopened.Close()
	}

	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Open(corrupt active generation) error = %v, want ErrCorrupt", err)
	}

	assertRawSemanticKeyExists(t, directory, evidenceKey)
}

func assertRawSemanticKeyExists(t *testing.T, directory string, key []byte) {
	t.Helper()

	database, err := badger.Open(badger.DefaultOptions(directory).WithLogger(nil))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, database.Close()) })

	internalRequireNoError(t, database.View(func(transaction *badger.Txn) error {
		_, err := transaction.Get(key)

		return err
	}))
}

func TestStoredKeysHideIdentifiersAndVectorIsBinaryEncoded(t *testing.T) {
	t.Parallel()

	store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

	generation, coverage, chunks := internalFixture("secret-build-id", "secret-bleve-id", 1)
	_, err = store.Stage(context.Background(), generation, coverage, chunks)
	internalRequireNoError(t, err)

	internalRequireNoError(t, store.db.View(func(transaction *badger.Txn) error {
		iterator := transaction.NewIterator(badger.DefaultIteratorOptions)
		defer iterator.Close()

		for iterator.Rewind(); iterator.Valid(); iterator.Next() {
			key := iterator.Item().Key()
			if bytes.Contains(key, []byte(generation.Scope.Key)) || bytes.Contains(key, []byte(generation.BuildID)) ||
				bytes.Contains(key, []byte(chunks[0].RevisionID)) {
				t.Fatalf("semantic key contains a plaintext identifier: %q", key)
			}

			value, err := readItemValue(iterator.Item())
			if err != nil {
				return err
			}

			if iterator.Item().Key()[2] == keyChunk && bytes.Contains(value, []byte(`"vector":[1`)) {
				t.Fatal("vector was serialized as plaintext JSON numbers")
			}
		}

		return nil
	}))
}

func TestEncodedBytesEqualsActualStoredGenerationBytes(t *testing.T) {
	t.Parallel()

	store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

	generation, coverage, chunks := internalFixture("sized", "bleve-1", 1)
	state, err := store.Stage(context.Background(), generation, coverage, chunks)
	internalRequireNoError(t, err)

	var actual uint64

	internalRequireNoError(t, store.db.View(func(transaction *badger.Txn) error {
		stateItem, err := transaction.Get(stateKey(generation.Scope, generation.BuildID))
		if err != nil {
			return err
		}

		stateValue, err := readItemValue(stateItem)
		if err != nil {
			return err
		}

		actual = uint64(len(stateItem.Key()) + len(stateValue))

		iterator := transaction.NewIterator(badger.DefaultIteratorOptions)
		defer iterator.Close()

		prefix := chunkPrefix(generation.Scope, generation.BuildID)
		for iterator.Seek(prefix); iterator.ValidForPrefix(prefix); iterator.Next() {
			value, err := readItemValue(iterator.Item())
			if err != nil {
				return err
			}

			actual += uint64(len(iterator.Item().Key()) + len(value))
		}

		return nil
	}))

	if state.EncodedBytes != actual {
		t.Fatalf("EncodedBytes = %d, actual stored generation bytes = %d", state.EncodedBytes, actual)
	}
}

func internalFixture(buildID, lexicalGeneration string, watermark uint64) (Generation, []CoverageRecord, []Chunk) {
	scope := memory.Scope{Key: "test:scope", Kind: memory.ScopeKindPersonal}
	generation := Generation{
		Scope: scope, BuildID: buildID, LexicalGenerationID: lexicalGeneration,
		LexicalWatermark: watermark, ModelSpace: internalHash("a"), Dimensions: 2,
		ReadyAt: time.Date(2026, time.October, 15, 1, 2, 3, 0, time.UTC),
	}
	coverage := []CoverageRecord{{
		ItemID: "item", RevisionID: "revision", Revision: 1,
		ContentHash: internalHash("b"), ChunkCount: 1,
	}}
	chunks := []Chunk{{
		Scope: scope, BuildID: buildID, LexicalGenerationID: lexicalGeneration,
		LexicalWatermark: watermark, ModelSpace: generation.ModelSpace, Dimensions: 2,
		ItemID: "item", RevisionID: "revision", Revision: 1, ContentHash: coverage[0].ContentHash,
		InputHash: internalHash("c"), ScopeChangeSeq: watermark, Kind: memory.MemoryKindState,
		CreatedAt: generation.ReadyAt, Vector: []float32{1, 0},
	}}

	return generation, coverage, chunks
}

func internalHash(character string) string {
	return string(bytes.Repeat([]byte(character), 64))
}

func internalRequireNoError(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
