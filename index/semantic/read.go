package semantic

import (
	"context"
	"encoding/binary"
	"fmt"

	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func readItemValue(item *badger.Item) ([]byte, error) {
	return readItemValueBounded(item, semanticcore.ProjectionByteLimit)
}

func readItemValueBounded(item *badger.Item, maximum int) ([]byte, error) {
	valueSize := item.ValueSize()
	if valueSize < 0 || valueSize > int64(maximum)+badgerValueSizeOverestimate {
		return nil, ErrCorrupt
	}

	var result []byte

	err := item.Value(func(value []byte) error {
		if len(value) > maximum {
			return ErrCorrupt
		}

		result = append([]byte(nil), value...)

		return nil
	})

	return result, err
}

func loadPointer(transaction *badger.Txn, scope memory.Scope) (activePointer, bool, error) {
	item, err := transaction.Get(activeKey(scope))
	if err == badger.ErrKeyNotFound {
		return activePointer{}, false, nil
	}

	if err != nil {
		return activePointer{}, false, err
	}

	value, err := readItemValueBounded(item, maxActiveValueBytes)
	if err != nil {
		return activePointer{}, false, err
	}

	var pointer activePointer
	if err := decodeJSON(value, &pointer); err != nil || pointer.SchemaVersion != schemaVersion ||
		pointer.ScopeKey != scope.Key || pointer.ScopeKind != string(scope.Kind) ||
		!validIdentity(pointer.BuildID) || pointer.Epoch == 0 {
		return activePointer{}, false, ErrCorrupt
	}

	return pointer, true, nil
}

func loadStoredState(transaction *badger.Txn, scope memory.Scope, buildID string) (storedState, error) {
	item, err := transaction.Get(stateKey(scope, buildID))
	if err != nil {
		if err == badger.ErrKeyNotFound {
			return storedState{}, ErrCorrupt
		}

		return storedState{}, err
	}

	value, err := readItemValueBounded(item, maxStateValueBytes)
	if err != nil {
		return storedState{}, err
	}

	var record storedState
	if err := decodeJSON(value, &record); err != nil || record.SchemaVersion != schemaVersion ||
		record.State.SchemaVersion != schemaVersion || record.State.Scope != scope || record.State.BuildID != buildID {
		return storedState{}, ErrCorrupt
	}

	if err := preflightStoredState(record.State); err != nil {
		return storedState{}, err
	}

	return record, nil
}

func preflightStoredState(state State) error {
	if state.ChunkCount > semanticcore.ProjectionChunkLimit || state.RecordCount > state.ChunkCount ||
		state.EncodedBytes > uint64(semanticcore.ProjectionByteLimit) {
		return ErrCorrupt
	}

	return nil
}

func loadValidatedGeneration(ctx context.Context, transaction *badger.Txn, record storedState) ([]Chunk, error) {
	state := record.State
	if err := validateReadyState(record); err != nil {
		return nil, ErrCorrupt
	}

	chunks, encodedBytes, err := scanGenerationChunks(ctx, transaction, state)
	if err != nil {
		return nil, err
	}

	return verifyGenerationState(ctx, state, chunks, encodedBytes)
}

func validateReadyState(record storedState) error {
	if !record.Complete || record.State.Status != StatusReady || record.State.SafeReason != "" {
		return ErrCorrupt
	}

	return validateGeneration(stateFromGeneration(record.State))
}

//nolint:cyclop,funlen // Every pre-copy and decoded-row corruption check is intentionally explicit.
func scanGenerationChunks(ctx context.Context, transaction *badger.Txn, state State) ([]Chunk, uint64, error) {
	remaining, err := generationRowBudget(state)
	if err != nil {
		return nil, 0, err
	}

	iterator := transaction.NewIterator(badger.DefaultIteratorOptions)
	defer iterator.Close()

	prefix := chunkPrefix(state.Scope, state.BuildID)
	chunks := make([]Chunk, 0, state.ChunkCount)
	encodedBytes := uint64(0)

	var (
		previousRevisionID string
		expectedOrdinal    uint32
	)

	for iterator.Seek(prefix); iterator.ValidForPrefix(prefix); iterator.Next() {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}

		item := iterator.Item()

		key := item.Key()
		if len(key) != len(prefix)+keyIndexSize ||
			binary.BigEndian.Uint32(key[len(prefix):]) != uint32(len(chunks)) {
			return nil, 0, ErrCorrupt
		}

		valueSize := item.ValueSize()
		if valueSize < 0 {
			return nil, 0, ErrCorrupt
		}

		keyBytes := uint64(len(key))
		if keyBytes > remaining || uint64(valueSize) > remaining-keyBytes+badgerValueSizeOverestimate {
			return nil, 0, ErrCorrupt
		}

		value, err := readItemValueBounded(item, min(maxChunkValueBytes, int(remaining-keyBytes)))
		if err != nil {
			return nil, 0, err
		}

		rowBytes := keyBytes + uint64(len(value))
		if rowBytes > remaining {
			return nil, 0, ErrCorrupt
		}

		chunk, err := decodeChunk(value)
		if err != nil {
			return nil, 0, err
		}

		if chunk.RevisionID != previousRevisionID {
			previousRevisionID = chunk.RevisionID
			expectedOrdinal = 0
		}

		if chunk.Ordinal != expectedOrdinal {
			return nil, 0, ErrCorrupt
		}

		expectedOrdinal++

		chunks = append(chunks, chunk)

		encodedBytes += rowBytes
		remaining -= rowBytes

		if len(chunks) > int(state.ChunkCount) || len(chunks) > semanticcore.ProjectionChunkLimit {
			return nil, 0, ErrCorrupt
		}
	}

	if remaining != 0 {
		return nil, 0, ErrCorrupt
	}

	return chunks, encodedBytes, nil
}

func generationRowBudget(state State) (uint64, error) {
	stateValue, err := encodeJSON(storedState{SchemaVersion: schemaVersion, Complete: true, State: state})
	if err != nil {
		return 0, err
	}

	stateBytes := uint64(len(stateKey(state.Scope, state.BuildID)) + len(stateValue))
	if state.EncodedBytes < stateBytes {
		return 0, ErrCorrupt
	}

	return state.EncodedBytes - stateBytes, nil
}

func verifyGenerationState(ctx context.Context, state State, chunks []Chunk, encodedBytes uint64) ([]Chunk, error) {
	coverage := coverageFromChunks(chunks)

	orderedCoverage, orderedChunks, coverageHash, err := validateAndOrder(
		ctx,
		stateFromGeneration(state),
		coverage,
		chunks,
	)
	if err != nil {
		return nil, ErrCorrupt
	}

	if !stateCountsMatch(state, orderedCoverage, orderedChunks, coverageHash) {
		return nil, ErrCorrupt
	}

	stateValue, err := encodeJSON(storedState{SchemaVersion: schemaVersion, Complete: true, State: state})
	if err != nil {
		return nil, err
	}

	encodedBytes += uint64(len(stateKey(state.Scope, state.BuildID)) + len(stateValue))
	if encodedBytes != state.EncodedBytes || CheckProjectionBounds(len(chunks), encodedBytes) != nil {
		return nil, ErrCorrupt
	}

	return orderedChunks, nil
}

func stateCountsMatch(state State, coverage []CoverageRecord, chunks []Chunk, coverageHash string) bool {
	return len(chunks) == int(state.ChunkCount) && len(coverage) == int(state.RecordCount) &&
		coverageHash == state.CoverageHash
}

func coverageFromChunks(chunks []Chunk) []CoverageRecord {
	coverage := make([]CoverageRecord, 0)
	for _, chunk := range chunks {
		if len(coverage) == 0 || coverage[len(coverage)-1].RevisionID != chunk.RevisionID {
			coverage = append(coverage, CoverageRecord{
				ItemID: chunk.ItemID, RevisionID: chunk.RevisionID, Revision: chunk.Revision,
				ContentHash: chunk.ContentHash,
			})
		}

		coverage[len(coverage)-1].ChunkCount++
	}

	return coverage
}

// Snapshot opens one complete compatible immutable generation.
func (store *Store) Snapshot(ctx context.Context, view View) (*Snapshot, bool, error) {
	if ctx == nil || validateView(view) != nil {
		return nil, false, ErrInvalid
	}

	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	unlock, err := store.gate.acquire(ctx, false)
	if err != nil {
		return nil, false, err
	}
	defer unlock()

	if err := store.ensureOpen(); err != nil {
		return nil, false, err
	}

	var (
		snapshot *Snapshot
		found    bool
	)

	err = store.db.View(func(transaction *badger.Txn) error {
		var err error

		snapshot, found, err = loadSnapshot(ctx, transaction, view)

		return err
	})
	if err != nil {
		return nil, false, fmt.Errorf("read semantic snapshot: %w", err)
	}

	return snapshot, found, nil
}

func loadSnapshot(ctx context.Context, transaction *badger.Txn, view View) (*Snapshot, bool, error) {
	pointer, exists, err := loadPointer(transaction, view.Scope)
	if err != nil || !exists {
		return nil, false, err
	}

	record, err := loadStoredState(transaction, view.Scope, pointer.BuildID)
	if err != nil {
		return nil, false, err
	}

	if record.State.Status != StatusReady || !stateMatchesView(record.State, view) {
		return nil, false, nil
	}

	if record.State.PublicationEpoch != pointer.Epoch {
		return nil, false, ErrCorrupt
	}

	chunks, err := loadValidatedGeneration(ctx, transaction, record)
	if err != nil {
		return nil, false, err
	}

	return &Snapshot{state: record.State, chunks: chunks}, true, nil
}

// ActiveState returns the exact ready or degraded marker currently advertised
// for a scope. Returned state is validated before it is exposed.
func (store *Store) ActiveState(ctx context.Context, scope memory.Scope) (State, bool, error) {
	if ctx == nil || validateScope(scope) != nil {
		return State{}, false, ErrInvalid
	}

	if err := ctx.Err(); err != nil {
		return State{}, false, err
	}

	unlock, err := store.gate.acquire(ctx, false)
	if err != nil {
		return State{}, false, err
	}
	defer unlock()

	if err := store.ensureOpen(); err != nil {
		return State{}, false, err
	}

	var (
		state State
		found bool
	)

	err = store.db.View(func(transaction *badger.Txn) error {
		var err error

		state, found, err = loadActiveState(ctx, transaction, scope)

		return err
	})
	if err != nil {
		return State{}, false, fmt.Errorf("read active semantic state: %w", err)
	}

	return state, found, nil
}

func loadActiveState(ctx context.Context, transaction *badger.Txn, scope memory.Scope) (State, bool, error) {
	pointer, exists, err := loadPointer(transaction, scope)
	if err != nil || !exists {
		return State{}, false, err
	}

	record, err := loadStoredState(transaction, scope, pointer.BuildID)
	if err != nil {
		return State{}, false, err
	}

	if err := validateActiveRecord(ctx, transaction, record); err != nil {
		return State{}, false, err
	}

	if record.State.PublicationEpoch != pointer.Epoch {
		return State{}, false, ErrCorrupt
	}

	return record.State, true, nil
}

func validateActiveRecord(ctx context.Context, transaction *badger.Txn, record storedState) error {
	switch record.State.Status {
	case StatusReady:
		_, err := loadValidatedGeneration(ctx, transaction, record)

		return err
	case StatusDegraded:
		return validateDegradedState(record)
	default:
		return ErrCorrupt
	}
}

func validateDegradedState(record storedState) error {
	state := record.State

	reason := semanticcore.Reason(state.SafeReason)
	if err := validateDegradedHeader(record, reason); err != nil {
		return ErrCorrupt
	}

	if !emptyDegradedCounts(state) {
		return ErrCorrupt
	}

	encoded, err := encodeJSON(storedState{SchemaVersion: schemaVersion, Complete: true, State: state})
	if err != nil {
		return err
	}

	expected := uint64(len(stateKey(state.Scope, state.BuildID)) + len(encoded))
	if state.EncodedBytes != expected || CheckProjectionBounds(0, expected) != nil {
		return ErrCorrupt
	}

	return nil
}

func validateDegradedHeader(record storedState, reason semanticcore.Reason) error {
	if !record.Complete {
		return ErrCorrupt
	}

	if validateGeneration(stateFromGeneration(record.State)) != nil {
		return ErrCorrupt
	}

	if record.State.Status != StatusDegraded {
		return ErrCorrupt
	}

	if !reason.Valid() {
		return ErrCorrupt
	}

	return nil
}

func emptyDegradedCounts(state State) bool {
	return state.ChunkCount == 0 && state.RecordCount == 0 &&
		state.CoverageHash == coverageFingerprint(nil)
}
