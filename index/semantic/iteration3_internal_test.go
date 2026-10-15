package semantic

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dgraph-io/badger/v4"
)

func TestGenerationScanRejectsAggregateOverBudgetBeforeLaterRowCopy(t *testing.T) {
	t.Parallel()

	store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

	generation, coverage, chunks := internalManyChunkFixture("aggregate-corrupt", "bleve-1", 1, 16)
	_, err = store.Stage(context.Background(), generation, coverage, chunks)
	internalRequireNoError(t, err)

	internalRequireNoError(t, store.db.Update(func(transaction *badger.Txn) error {
		item, getErr := transaction.Get(stateKey(generation.Scope, generation.BuildID))
		if getErr != nil {
			return getErr
		}

		value, readErr := readItemValueBounded(item, maxStateValueBytes)
		if readErr != nil {
			return readErr
		}

		var record storedState
		if decodeErr := decodeJSON(value, &record); decodeErr != nil {
			return decodeErr
		}

		first, getErr := transaction.Get(chunkKey(generation.Scope, generation.BuildID, 0))
		if getErr != nil {
			return getErr
		}

		firstRowBytes := uint64(len(first.Key())) + uint64(first.ValueSize())
		record.State = stateWithRowBudget(t, record.State, firstRowBytes)

		value, encodeErr := encodeJSON(record)
		if encodeErr != nil {
			return encodeErr
		}

		return transaction.Set(stateKey(generation.Scope, generation.BuildID), value)
	}))

	err = store.db.View(func(transaction *badger.Txn) error {
		record, loadErr := loadStoredState(transaction, generation.Scope, generation.BuildID)
		if loadErr != nil {
			return loadErr
		}

		_, loadErr = loadValidatedGeneration(newCancelAfterContext(3), transaction, record)

		return loadErr
	})
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("loadValidatedGeneration() error = %v, want ErrCorrupt before later-row cancellation", err)
	}
}

func TestMaxEscapedMetadataStagesPublishesAndRoundTrips(t *testing.T) {
	t.Parallel()

	store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

	generation, coverage, chunks := internalFixture("escaped-max", "bleve-1", 1)

	ids := make([]string, maxMetadataIDs)
	for index := range ids {
		ids[index] = fmt.Sprintf("%03d", index) + strings.Repeat("<>&", 169) + "<>"
	}

	chunks[0].SourceIDs = append([]string(nil), ids...)
	chunks[0].ConversationIDs = append([]string(nil), ids...)
	encoded, err := encodeChunk(chunks[0])
	internalRequireNoError(t, err)

	if len(encoded) > maxChunkValueBytes {
		t.Fatalf("maximum valid escaped chunk encodes to %d bytes, reader cap is %d", len(encoded), maxChunkValueBytes)
	}

	_, err = store.Stage(context.Background(), generation, coverage, chunks)
	internalRequireNoError(t, err)
	internalRequireNoError(t, store.Publish(context.Background(), Publication{
		View: generation.View(), BuildID: generation.BuildID,
	}))
	snapshot, found, err := store.Snapshot(context.Background(), generation.View())
	internalRequireNoError(t, err)

	if !found || len(snapshot.Chunks()) != 1 || len(snapshot.Chunks()[0].SourceIDs) != maxMetadataIDs {
		t.Fatalf("Snapshot(max escaped metadata) = (%#v, %t), want one complete chunk", snapshot, found)
	}

	over, overCoverage, overChunks := internalFixture("escaped-over", "bleve-2", 2)

	overChunks[0].SourceIDs = []string{ids[0] + "x"}
	if _, err := store.Stage(context.Background(), over, overCoverage, overChunks); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Stage(escaped metadata field at 513 bytes) error = %v, want ErrInvalid", err)
	}
}

func stateWithRowBudget(t *testing.T, state State, rowBytes uint64) State {
	t.Helper()

	state.EncodedBytes = 0
	for range encodedSizeConvergenceLimit {
		encoded, err := encodeJSON(storedState{SchemaVersion: schemaVersion, Complete: true, State: state})
		internalRequireNoError(t, err)

		total := uint64(len(stateKey(state.Scope, state.BuildID))+len(encoded)) + rowBytes
		if total == state.EncodedBytes {
			return state
		}

		state.EncodedBytes = total
	}

	t.Fatal("corrupt state row budget did not converge")

	return State{}
}

//nolint:funlen // The complete A-B-GC-A history must remain visible in one ABA regression.
func TestPublicationEpochAllowsBuildReuseWithoutABA(t *testing.T) {
	t.Parallel()

	store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

	first, firstCoverage, firstChunks := internalFixture("build-a", "bleve-1", 1)
	firstState, err := store.Stage(context.Background(), first, firstCoverage, firstChunks)
	internalRequireNoError(t, err)

	if firstState.PublicationEpoch != 0 {
		t.Fatalf("first Stage() epoch = %d, want unpublished epoch 0", firstState.PublicationEpoch)
	}

	internalRequireNoError(t, store.Publish(context.Background(), Publication{
		View: first.View(), BuildID: first.BuildID,
	}))
	firstState, _, err = store.ActiveState(context.Background(), first.Scope)
	internalRequireNoError(t, err)

	if firstState.PublicationEpoch != 1 {
		t.Fatalf("first active epoch = %d, want 1", firstState.PublicationEpoch)
	}

	second, secondCoverage, secondChunks := internalFixture("build-b", "bleve-2", 2)
	secondState, err := store.Stage(context.Background(), second, secondCoverage, secondChunks)
	internalRequireNoError(t, err)

	if secondState.PublicationEpoch != 0 {
		t.Fatalf("second Stage() epoch = %d, want unpublished epoch 0", secondState.PublicationEpoch)
	}

	internalRequireNoError(t, store.Publish(context.Background(), Publication{
		View: second.View(), BuildID: second.BuildID,
		ExpectedActiveBuildID: first.BuildID, ExpectedActiveEpoch: firstState.PublicationEpoch,
	}))
	secondState, _, err = store.ActiveState(context.Background(), second.Scope)
	internalRequireNoError(t, err)

	if secondState.PublicationEpoch != 2 {
		t.Fatalf("second active epoch = %d, want 2", secondState.PublicationEpoch)
	}

	internalRequireNoError(t, store.CollectGarbage(context.Background()))

	reused, reusedCoverage, reusedChunks := internalFixture("build-a", "bleve-3", 3)
	reusedState, err := store.Stage(context.Background(), reused, reusedCoverage, reusedChunks)
	internalRequireNoError(t, err)

	if reusedState.PublicationEpoch != 0 {
		t.Fatalf("reused Stage() epoch = %d, want unpublished epoch 0", reusedState.PublicationEpoch)
	}

	internalRequireNoError(t, store.Publish(context.Background(), Publication{
		View: reused.View(), BuildID: reused.BuildID,
		ExpectedActiveBuildID: second.BuildID, ExpectedActiveEpoch: secondState.PublicationEpoch,
	}))
	reusedState, _, err = store.ActiveState(context.Background(), reused.Scope)
	internalRequireNoError(t, err)

	if reusedState.PublicationEpoch != 3 {
		t.Fatalf("reused active epoch = %d, want 3", reusedState.PublicationEpoch)
	}

	stale, staleCoverage, staleChunks := internalFixture("build-stale", "bleve-4", 4)
	_, err = store.Stage(context.Background(), stale, staleCoverage, staleChunks)
	internalRequireNoError(t, err)

	err = store.Publish(context.Background(), Publication{
		View: stale.View(), BuildID: stale.BuildID,
		ExpectedActiveBuildID: first.BuildID, ExpectedActiveEpoch: firstState.PublicationEpoch,
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Publish(stale A@1 against A@3) error = %v, want ErrConflict", err)
	}
}

func TestSnapshotRejectsCorruptPublicationEpoch(t *testing.T) {
	for name, epoch := range map[string]uint64{"zero": 0, "state mismatch": 2} {
		t.Run(name, func(t *testing.T) {
			store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
			internalRequireNoError(t, err)
			t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

			generation, coverage, chunks := internalFixture("epoch-corrupt", "bleve-1", 1)
			_, err = store.Stage(context.Background(), generation, coverage, chunks)
			internalRequireNoError(t, err)
			internalRequireNoError(t, store.Publish(context.Background(), Publication{
				View: generation.View(), BuildID: generation.BuildID,
			}))

			value, encodeErr := encodeJSON(activePointer{
				SchemaVersion: schemaVersion, ScopeKey: generation.Scope.Key,
				ScopeKind: string(generation.Scope.Kind), BuildID: generation.BuildID, Epoch: epoch,
			})
			internalRequireNoError(t, encodeErr)
			internalRequireNoError(t, store.db.Update(func(transaction *badger.Txn) error {
				return transaction.Set(activeKey(generation.Scope), value)
			}))

			if _, _, err := store.Snapshot(context.Background(), generation.View()); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("Snapshot(pointer epoch %d) error = %v, want ErrCorrupt", epoch, err)
			}
		})
	}
}

//nolint:cyclop,funlen // Corrupt-state setup stays inline so the overflow invariant is auditable.
func TestPublicationEpochOverflowRejectsWithoutChangingPointer(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

	active, activeCoverage, activeChunks := internalFixture("epoch-max", "bleve-1", 1)
	_, err = store.Stage(context.Background(), active, activeCoverage, activeChunks)
	internalRequireNoError(t, err)
	internalRequireNoError(t, store.Publish(context.Background(), Publication{
		View: active.View(), BuildID: active.BuildID,
	}))

	const maximumEpoch = ^uint64(0)

	internalRequireNoError(t, store.db.Update(func(transaction *badger.Txn) error {
		record, loadErr := loadStoredState(transaction, active.Scope, active.BuildID)
		if loadErr != nil {
			return loadErr
		}

		rowBytes, budgetErr := generationRowBudget(record.State)
		if budgetErr != nil {
			return budgetErr
		}

		record.State.PublicationEpoch = maximumEpoch

		record.State, loadErr = finalizeEncodedBytes(record.State, rowBytes)
		if loadErr != nil {
			return loadErr
		}

		stateValue, encodeErr := encodeJSON(record)
		if encodeErr != nil {
			return encodeErr
		}

		pointerValue, encodeErr := encodeJSON(activePointer{
			SchemaVersion: schemaVersion, ScopeKey: active.Scope.Key,
			ScopeKind: string(active.Scope.Kind), BuildID: active.BuildID, Epoch: maximumEpoch,
		})
		if encodeErr != nil {
			return encodeErr
		}

		if setErr := transaction.Set(stateKey(active.Scope, active.BuildID), stateValue); setErr != nil {
			return setErr
		}

		return transaction.Set(activeKey(active.Scope), pointerValue)
	}))

	candidate, candidateCoverage, candidateChunks := internalFixture("epoch-next", "bleve-2", 2)
	_, err = store.Stage(context.Background(), candidate, candidateCoverage, candidateChunks)
	internalRequireNoError(t, err)

	err = store.Publish(context.Background(), Publication{
		View: candidate.View(), BuildID: candidate.BuildID,
		ExpectedActiveBuildID: active.BuildID, ExpectedActiveEpoch: maximumEpoch,
	})
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("Publish(after maximum epoch) error = %v, want ErrCapacity", err)
	}

	state, found, err := store.ActiveState(context.Background(), active.Scope)
	internalRequireNoError(t, err)

	if !found || state.BuildID != active.BuildID || state.PublicationEpoch != maximumEpoch {
		t.Fatalf("ActiveState() after overflow = (%#v, %t), want unchanged maximum epoch", state, found)
	}
}

func TestGarbageCollectionRemovesLegacyBuildClaims(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

	generation, _, _ := internalFixture("legacy-claim", "bleve-1", 1)
	key := legacyUsedBuildKey(generation.Scope, generation.BuildID)

	internalRequireNoError(t, store.db.Update(func(transaction *badger.Txn) error {
		return transaction.Set(key, []byte{1})
	}))
	internalRequireNoError(t, store.CollectGarbage(context.Background()))
	internalRequireNoError(t, store.db.View(func(transaction *badger.Txn) error {
		if _, getErr := transaction.Get(key); !errors.Is(getErr, badger.ErrKeyNotFound) {
			t.Fatalf("legacy used-build claim remains after GC: %v", getErr)
		}

		return nil
	}))
}
