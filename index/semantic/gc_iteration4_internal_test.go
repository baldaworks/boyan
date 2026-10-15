package semantic

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

var errGarbageCadenceExceeded = errors.New("garbage classification cadence exceeded")

type garbageCadenceContext struct {
	checks int
	limit  int
}

func (*garbageCadenceContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*garbageCadenceContext) Done() <-chan struct{}       { return nil }
func (*garbageCadenceContext) Value(any) any               { return nil }

func (ctx *garbageCadenceContext) Err() error {
	ctx.checks++
	if ctx.checks > ctx.limit {
		return errGarbageCadenceExceeded
	}

	return nil
}

func (ctx *garbageCadenceContext) reset() {
	ctx.checks = 0
}

//nolint:funlen // Ready and degraded retention belong in one cross-status GC regression.
func TestGarbageCollectionDeletesReadySurplusAndAllDegradedChunks(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

	ready, coverage, chunks := internalFixture("ready", "bleve-ready", 1)
	_, err = store.Stage(context.Background(), ready, coverage, chunks)
	internalRequireNoError(t, err)
	internalRequireNoError(t, store.Publish(context.Background(), Publication{
		View: ready.View(), BuildID: ready.BuildID,
	}))

	degraded := Generation{
		Scope:   memory.Scope{Key: "test:degraded", Kind: memory.ScopeKindPersonal},
		BuildID: "degraded", LexicalGenerationID: "bleve-degraded", LexicalWatermark: 2,
		ModelSpace: ready.ModelSpace, Dimensions: ready.Dimensions, ReadyAt: ready.ReadyAt,
	}
	_, err = store.PublishDegraded(context.Background(), Degraded{
		Generation: degraded, SafeReason: semanticcore.ReasonProjectionCapacity,
	}, "")
	internalRequireNoError(t, err)

	readyValue, err := encodeChunk(chunks[0])
	internalRequireNoError(t, err)

	degradedChunk := chunks[0]
	degradedChunk.Scope = degraded.Scope
	degradedChunk.BuildID = degraded.BuildID
	degradedChunk.LexicalGenerationID = degraded.LexicalGenerationID
	degradedChunk.LexicalWatermark = degraded.LexicalWatermark
	degradedChunk.ScopeChangeSeq = degraded.LexicalWatermark
	degradedValue, err := encodeChunk(degradedChunk)
	internalRequireNoError(t, err)

	readySurplusKey := chunkKey(ready.Scope, ready.BuildID, 1)
	readyFarSurplusKey := chunkKey(ready.Scope, ready.BuildID, 99)
	degradedChunkKey := chunkKey(degraded.Scope, degraded.BuildID, 0)

	internalRequireNoError(t, store.db.Update(func(transaction *badger.Txn) error {
		if setErr := transaction.Set(readySurplusKey, readyValue); setErr != nil {
			return setErr
		}

		if setErr := transaction.Set(readyFarSurplusKey, readyValue); setErr != nil {
			return setErr
		}

		return transaction.Set(degradedChunkKey, degradedValue)
	}))

	internalRequireNoError(t, store.CollectGarbage(context.Background()))
	internalRequireNoError(t, store.db.View(func(transaction *badger.Txn) error {
		if _, getErr := transaction.Get(chunkKey(ready.Scope, ready.BuildID, 0)); getErr != nil {
			t.Fatalf("valid ready chunk removed: %v", getErr)
		}

		for name, key := range map[string][]byte{
			"ready boundary surplus": readySurplusKey,
			"ready far surplus":      readyFarSurplusKey,
			"degraded":               degradedChunkKey,
		} {
			if _, getErr := transaction.Get(key); !errors.Is(getErr, badger.ErrKeyNotFound) {
				t.Fatalf("%s chunk remains after GC: %v", name, getErr)
			}
		}

		return nil
	}))
}

//nolint:cyclop,funlen // The multi-page fixture and preservation assertions intentionally stay together.
func TestGarbageCollectionClassifiesManyActiveScopesInBoundedReadBatches(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

	const activeScopes = garbageDeleteBatch + 205

	readyAt := time.Date(2026, time.October, 15, 1, 2, 3, 0, time.UTC)

	for start := 0; start < activeScopes; start += 100 {
		end := min(start+100, activeScopes)

		internalRequireNoError(t, store.db.Update(func(transaction *badger.Txn) error {
			for index := start; index < end; index++ {
				generation := Generation{
					Scope: memory.Scope{
						Key: fmt.Sprintf("test:gc-active:%04d", index), Kind: memory.ScopeKindPersonal,
					},
					BuildID: fmt.Sprintf("build-%04d", index), LexicalGenerationID: "bleve-active",
					LexicalWatermark: 1, ModelSpace: internalHash("a"), Dimensions: 2, ReadyAt: readyAt,
				}

				state, stateValue, pointerValue, encodeErr := encodeDegraded(Degraded{
					Generation: generation, SafeReason: semanticcore.ReasonProjectionCapacity,
				}, 1)
				if encodeErr != nil {
					return encodeErr
				}

				if setErr := transaction.Set(stateKey(state.Scope, state.BuildID), stateValue); setErr != nil {
					return setErr
				}

				if setErr := transaction.Set(activeKey(state.Scope), pointerValue); setErr != nil {
					return setErr
				}
			}

			return nil
		}))
	}

	firstScope := memory.Scope{Key: "test:gc-active:0000", Kind: memory.ScopeKindPersonal}
	if _, found, activeErr := store.ActiveState(context.Background(), firstScope); activeErr != nil || !found {
		t.Fatalf("seeded ActiveState() = (_, %t, %v), want valid", found, activeErr)
	}

	ctx := &garbageCadenceContext{limit: garbageDeleteBatch + 20}
	scanBatches := 0
	store.afterGarbageScanBatch = func(size int) {
		if size > garbageDeleteBatch {
			t.Fatalf("GC scan batch = %d, want <= %d", size, garbageDeleteBatch)
		}

		scanBatches++

		ctx.reset()
	}

	if err := store.CollectGarbage(ctx); err != nil {
		t.Fatalf("CollectGarbage(%d active scopes) error = %v", activeScopes, err)
	}

	if scanBatches < 2 {
		t.Fatalf("GC scan batches = %d, want multiple bounded batches", scanBatches)
	}

	if got := countPrefixKeys(t, store, statePrefix()); got != activeScopes {
		t.Fatalf("active state keys after multi-batch GC = %d, want %d", got, activeScopes)
	}

	if got := countPrefixKeys(t, store, []byte{keyNamespace, keyVersion, keyActive}); got != activeScopes {
		t.Fatalf("active pointer keys after multi-batch GC = %d, want %d", got, activeScopes)
	}

	for _, index := range []int{0, activeScopes - 1} {
		scope := memory.Scope{
			Key: fmt.Sprintf("test:gc-active:%04d", index), Kind: memory.ScopeKindPersonal,
		}
		if _, found, activeErr := store.ActiveState(context.Background(), scope); activeErr != nil || !found {
			t.Fatalf("ActiveState(%d) after GC = (_, %t, %v), want preserved", index, found, activeErr)
		}
	}
}

func TestGarbageCollectionCancellationResumesWithoutSkippingOrDeletingActiveState(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

	active, coverage, chunks := internalFixture("gc-cancel-active", "bleve-active", 1)
	_, err = store.Stage(context.Background(), active, coverage, chunks)
	internalRequireNoError(t, err)
	internalRequireNoError(t, store.Publish(context.Background(), Publication{
		View: active.View(), BuildID: active.BuildID,
	}))

	const orphanCount = garbageDeleteBatch + 31
	for start := 0; start < orphanCount; start += 200 {
		end := min(start+200, orphanCount)

		internalRequireNoError(t, store.db.Update(func(transaction *badger.Txn) error {
			for index := start; index < end; index++ {
				buildID := fmt.Sprintf("cancel-orphan-%04d", index)
				if setErr := transaction.Set(stateKey(active.Scope, buildID), []byte("{}")); setErr != nil {
					return setErr
				}
			}

			return nil
		}))
	}

	ctx, cancel := context.WithCancel(context.Background())

	store.afterGarbageScanBatch = func(int) { cancel() }
	if err := store.CollectGarbage(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("CollectGarbage(canceled after first page) error = %v, want context.Canceled", err)
	}

	store.afterGarbageScanBatch = nil
	internalRequireNoError(t, store.CollectGarbage(context.Background()))

	if _, found, err := store.Snapshot(context.Background(), active.View()); err != nil || !found {
		t.Fatalf("Snapshot(active after canceled/resumed GC) = (_, %t, %v), want preserved", found, err)
	}

	if got := countPrefixKeys(t, store, statePrefix()); got != 1 {
		t.Fatalf("state keys after resumed GC = %d, want only active state", got)
	}
}

func TestGarbageCollectionFailsClosedOnCorruptActivePointer(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

	active, coverage, chunks := internalFixture("gc-corrupt-active", "bleve-active", 1)
	_, err = store.Stage(context.Background(), active, coverage, chunks)
	internalRequireNoError(t, err)
	internalRequireNoError(t, store.Publish(context.Background(), Publication{
		View: active.View(), BuildID: active.BuildID,
	}))

	orphanKey := stateKey(active.Scope, "gc-corrupt-orphan")

	internalRequireNoError(t, store.db.Update(func(transaction *badger.Txn) error {
		if setErr := transaction.Set(orphanKey, []byte("{}")); setErr != nil {
			return setErr
		}

		return transaction.Set(activeKey(active.Scope), []byte("{"))
	}))

	if err := store.CollectGarbage(context.Background()); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("CollectGarbage(corrupt active pointer) error = %v, want ErrCorrupt", err)
	}

	internalRequireNoError(t, store.db.View(func(transaction *badger.Txn) error {
		if _, getErr := transaction.Get(orphanKey); getErr != nil {
			t.Fatalf("GC deleted data after corrupt active pointer: %v", getErr)
		}

		return nil
	}))
}

func countPrefixKeys(t *testing.T, store *Store, prefix []byte) int {
	t.Helper()

	count := 0

	internalRequireNoError(t, store.db.View(func(transaction *badger.Txn) error {
		options := badger.DefaultIteratorOptions
		options.Prefix = prefix

		iterator := transaction.NewIterator(options)
		defer iterator.Close()

		for iterator.Rewind(); iterator.Valid(); iterator.Next() {
			count++
		}

		return nil
	}))

	return count
}
