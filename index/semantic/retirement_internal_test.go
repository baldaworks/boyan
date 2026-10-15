package semantic

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/dgraph-io/badger/v4"
)

// Replacing only the active pointer would retain twenty generation rows.
func TestRuntimePublicationsReclaimRetiredGenerations(t *testing.T) {
	t.Parallel()
	store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

	var (
		previous State
		held     *Snapshot
	)

	for index := range 10 {
		generation, coverage, chunks := internalFixture(
			fmt.Sprintf("build-%d", index), fmt.Sprintf("lex-%d", index), uint64(index+1),
		)
		_, err = store.Stage(context.Background(), generation, coverage, chunks)
		internalRequireNoError(t, err)
		internalRequireNoError(t, store.Publish(context.Background(), Publication{
			View: generation.View(), BuildID: generation.BuildID,
			ExpectedActiveBuildID: previous.BuildID, ExpectedActiveEpoch: previous.PublicationEpoch,
		}))
		previous, _, err = store.ActiveState(context.Background(), generation.Scope)
		internalRequireNoError(t, err)

		if index == 0 {
			held, _, err = store.Snapshot(context.Background(), generation.View())
			internalRequireNoError(t, err)
		}
	}

	if got := countPrefixKeys(t, store, []byte{keyNamespace, keyVersion}); got != 3 {
		t.Fatalf("ten runtime publications leave %d live rows, want 3", got)
	}

	if got := held.Chunks(); len(got) != 1 || got[0].BuildID != "build-0" || got[0].Vector[0] != 1 {
		t.Fatalf("held snapshot changed after retirement: %#v", got)
	}
}

//nolint:funlen // Both competing workers and the surviving stages form one publication scenario.
func TestRuntimeConcurrentPublicationsPreserveStagedLoserAndOtherScope(t *testing.T) {
	t.Parallel()
	store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

	active, coverage, chunks := internalFixture("active", "lex-1", 1)
	_, err = store.Stage(context.Background(), active, coverage, chunks)
	internalRequireNoError(t, err)
	internalRequireNoError(t, store.Publish(context.Background(), Publication{
		View: active.View(), BuildID: active.BuildID,
	}))

	other, otherCoverage, otherChunks := internalFixture("other", "lex-other", 1)
	other.Scope.Key = "test:other"
	otherChunks[0].Scope = other.Scope
	_, err = store.Stage(context.Background(), other, otherCoverage, otherChunks)
	internalRequireNoError(t, err)

	var (
		staged   [2]Generation
		failures [2]error
		workers  sync.WaitGroup
	)
	for index := range staged {
		workers.Add(1)
		go func() {
			defer workers.Done()

			generation, records, vectors := internalFixture(fmt.Sprintf("worker-%d", index), "lex-2", 2)
			staged[index] = generation
			_, failures[index] = store.Stage(context.Background(), generation, records, vectors)
		}()
	}

	workers.Wait()

	for _, err := range failures {
		internalRequireNoError(t, err)
	}

	for index := range staged {
		workers.Add(1)
		go func() {
			defer workers.Done()

			failures[index] = store.Publish(context.Background(), Publication{
				View: staged[index].View(), BuildID: staged[index].BuildID,
				ExpectedActiveBuildID: active.BuildID, ExpectedActiveEpoch: 1,
			})
		}()
	}

	workers.Wait()

	winner, loser := 0, 1
	if failures[0] != nil {
		winner, loser = 1, 0
	}

	internalRequireNoError(t, failures[winner])

	if !errors.Is(failures[loser], ErrConflict) {
		t.Fatalf("loser error = %v, want conflict", failures[loser])
	}

	internalRequireNoError(t, store.Publish(context.Background(), Publication{
		View: staged[loser].View(), BuildID: staged[loser].BuildID,
		ExpectedActiveBuildID: staged[winner].BuildID, ExpectedActiveEpoch: 2,
	}))
	internalRequireNoError(t, store.Publish(context.Background(), Publication{View: other.View(), BuildID: other.BuildID}))

	if got := countPrefixKeys(t, store, []byte{keyNamespace, keyVersion}); got != 6 {
		t.Fatalf("concurrent publication leaves %d rows, want two active generations (6)", got)
	}
}

func TestRuntimeRetirementCancellationRollsBackAndCanRetry(t *testing.T) {
	for _, degraded := range []bool{false, true} {
		t.Run(fmt.Sprintf("degraded-%t", degraded), func(t *testing.T) {
			store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
			internalRequireNoError(t, err)
			t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

			active, coverage, chunks := internalManyChunkFixture("active", "lex-1", 1, 100)
			_, err = store.Stage(context.Background(), active, coverage, chunks)
			internalRequireNoError(t, err)
			internalRequireNoError(t, store.Publish(context.Background(), Publication{
				View: active.View(), BuildID: active.BuildID,
			}))

			next, _, _ := internalFixture("next", "lex-2", 2)
			if !degraded {
				_, err = store.Stage(context.Background(), next, nil, nil)
				internalRequireNoError(t, err)
			}

			publish := func(ctx context.Context) error {
				if degraded {
					_, publishErr := store.PublishDegraded(ctx, Degraded{
						Generation: next, SafeReason: semanticcore.ReasonProjectionCapacity, ExpectedActiveEpoch: 1,
					}, active.BuildID)

					return publishErr
				}

				return store.Publish(ctx, Publication{
					View: next.View(), BuildID: next.BuildID, ExpectedActiveBuildID: active.BuildID, ExpectedActiveEpoch: 1,
				})
			}
			if err := publish(newCancelAfterContext(40)); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation during retirement = %v, want context.Canceled", err)
			}

			state, found, err := store.ActiveState(context.Background(), active.Scope)
			internalRequireNoError(t, err)

			if !found || state.BuildID != active.BuildID || state.PublicationEpoch != 1 {
				t.Fatalf("canceled retirement changed active state: %#v", state)
			}

			if got := countPrefixKeys(t, store, allChunkPrefix()); got != 100 {
				t.Fatalf("canceled retirement leaves %d old chunks, want 100", got)
			}

			internalRequireNoError(t, publish(context.Background()))

			if got := countPrefixKeys(t, store, []byte{keyNamespace, keyVersion}); got != 2 {
				t.Fatalf("retry leaves %d rows, want empty/degraded state and active pointer", got)
			}
		})
	}
}

func TestRuntimeRetirementRejectsCorruptPriorMetadata(t *testing.T) {
	for _, mutate := range []func(*storedState){
		func(record *storedState) { record.State.ChunkCount = semanticcore.ProjectionChunkLimit + 1 },
		func(record *storedState) { record.State.Status = "invalid" },
		func(record *storedState) { record.State.PublicationEpoch++ },
		func(record *storedState) { record.State.ChunkCount = 0; record.State.RecordCount = 0 },
	} {
		store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
		internalRequireNoError(t, err)
		t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

		active, coverage, chunks := internalFixture("active", "lex-1", 1)
		_, err = store.Stage(context.Background(), active, coverage, chunks)
		internalRequireNoError(t, err)
		internalRequireNoError(t, store.Publish(context.Background(), Publication{
			View: active.View(), BuildID: active.BuildID,
		}))
		internalRequireNoError(t, store.db.Update(func(transaction *badger.Txn) error {
			record, loadErr := loadStoredState(transaction, active.Scope, active.BuildID)
			if loadErr != nil {
				return loadErr
			}

			mutate(&record)

			value, encodeErr := encodeStoredState(record)
			if encodeErr != nil {
				return encodeErr
			}

			return transaction.Set(stateKey(active.Scope, active.BuildID), value)
		}))

		next, _, _ := internalFixture("next", "lex-2", 2)
		_, err = store.Stage(context.Background(), next, nil, nil)
		internalRequireNoError(t, err)

		if err := store.Publish(context.Background(), Publication{
			View: next.View(), BuildID: next.BuildID, ExpectedActiveBuildID: active.BuildID, ExpectedActiveEpoch: 1,
		}); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("retiring corrupt prior metadata = %v, want ErrCorrupt", err)
		}

		if got := countPrefixKeys(t, store, allChunkPrefix()); got != 1 {
			t.Fatalf("corruption removed evidence: %d chunks", got)
		}
	}
}

// Degradation must retire vectors without removing another build's staging.
func TestRuntimeDegradationReclaimsOnlyPriorActiveGeneration(t *testing.T) {
	t.Parallel()
	store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

	active, coverage, chunks := internalFixture("active", "lex-1", 1)
	_, err = store.Stage(context.Background(), active, coverage, chunks)
	internalRequireNoError(t, err)
	internalRequireNoError(t, store.Publish(context.Background(), Publication{
		View: active.View(), BuildID: active.BuildID,
	}))

	pending, coverage, chunks := internalFixture("pending", "lex-2", 2)
	_, err = store.Stage(context.Background(), pending, coverage, chunks)
	internalRequireNoError(t, err)

	marker, _, _ := internalFixture("degraded", "lex-2", 2)
	state, err := store.PublishDegraded(context.Background(), Degraded{
		Generation: marker, SafeReason: semanticcore.ReasonProjectionCapacity, ExpectedActiveEpoch: 1,
	}, active.BuildID)
	internalRequireNoError(t, err)

	if got := countPrefixKeys(t, store, allChunkPrefix()); got != 1 {
		t.Fatalf("degradation leaves %d vector rows, want pending vector only", got)
	}

	internalRequireNoError(t, store.Publish(context.Background(), Publication{
		View: pending.View(), BuildID: pending.BuildID,
		ExpectedActiveBuildID: state.BuildID, ExpectedActiveEpoch: state.PublicationEpoch,
	}))

	if got := countPrefixKeys(t, store, []byte{keyNamespace, keyVersion}); got != 3 {
		t.Fatalf("publication after degraded retirement leaves %d rows, want 3", got)
	}
}

// The maximum accepted generation must fit one atomic retirement transaction.
func TestRuntimeRetirementAtMaximumChunkCount(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

	active, coverage, original := internalFixture("maximum", "lex-1", 1)

	chunks := make([]Chunk, semanticcore.ProjectionChunkLimit)
	for index := range chunks {
		chunks[index] = original[0]
		chunks[index].Ordinal = uint32(index)
	}

	coverage[0].ChunkCount = uint32(len(chunks))
	_, err = store.Stage(context.Background(), active, coverage, chunks)
	internalRequireNoError(t, err)
	internalRequireNoError(t, store.Publish(context.Background(), Publication{
		View: active.View(), BuildID: active.BuildID,
	}))
	// Keep a physical MVCC reader open across publication, as well as public snapshots.
	reader := store.db.NewTransaction(false)
	defer reader.Discard()

	_, err = reader.Get(chunkKey(active.Scope, active.BuildID, 0))
	internalRequireNoError(t, err)

	next, _, _ := internalFixture("empty", "lex-2", 2)
	_, err = store.Stage(context.Background(), next, nil, nil)
	internalRequireNoError(t, err)
	internalRequireNoError(t, store.Publish(context.Background(), Publication{
		View: next.View(), BuildID: next.BuildID, ExpectedActiveBuildID: active.BuildID, ExpectedActiveEpoch: 1,
	}))

	if got := countPrefixKeys(t, store, allChunkPrefix()); got != 0 {
		t.Fatalf("maximum retirement leaves %d chunks, want zero", got)
	}

	item, err := reader.Get(chunkKey(active.Scope, active.BuildID, uint32(len(chunks)-1)))
	internalRequireNoError(t, err)
	internalRequireNoError(t, item.Value(func(value []byte) error {
		if len(value) == 0 {
			return badger.ErrKeyNotFound
		}

		return nil
	}))
}
