package semantic

import (
	"context"
	"fmt"

	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/dgraph-io/badger/v4"
)

const stageBatchBytes = 2 << 20

type encodedRow struct {
	key   []byte
	value []byte
}

// PreflightStage validates complete generation metadata and computes its exact
// encoded byte budget without writing state. Missing vectors are represented by
// one valid dimension-sized placeholder because encoded vector size depends on
// dimensions, not component values.
func PreflightStage(
	ctx context.Context,
	generation Generation,
	coverage []CoverageRecord,
	chunks []Chunk,
) (State, error) {
	if ctx == nil || validateGeneration(generation) != nil {
		return State{}, ErrInvalid
	}

	if err := CheckProjectionBounds(len(chunks), 0); err != nil {
		return State{}, err
	}

	placeholder := make([]float32, generation.Dimensions)
	placeholder[0] = 1
	preflightChunks := make([]Chunk, len(chunks))
	copy(preflightChunks, chunks)

	for index := range preflightChunks {
		if len(preflightChunks[index].Vector) == 0 {
			preflightChunks[index].Vector = placeholder
		}
	}

	if err := preflightStageInputs(ctx, generation, coverage, preflightChunks); err != nil {
		return State{}, err
	}

	state, _, err := prepareStage(ctx, generation, coverage, preflightChunks)

	return state, err
}

// Stage writes one complete generation under unreachable keys. It never
// changes the active pointer, including on cancellation or failure.
//
//nolint:cyclop // Validation and cancellation gates intentionally remain explicit.
func (store *Store) Stage(
	ctx context.Context,
	generation Generation,
	coverage []CoverageRecord,
	chunks []Chunk,
) (State, error) {
	if ctx == nil {
		return State{}, ErrInvalid
	}

	if err := ctx.Err(); err != nil {
		return State{}, err
	}

	if err := validateGeneration(generation); err != nil {
		return State{}, err
	}

	if err := CheckProjectionBounds(len(chunks), 0); err != nil {
		return State{}, err
	}

	if err := preflightStageInputs(ctx, generation, coverage, chunks); err != nil {
		return State{}, err
	}

	state, rows, err := prepareStage(ctx, generation, coverage, chunks)
	if err != nil {
		return State{}, err
	}

	unlock, err := store.gate.acquire(ctx, true)
	if err != nil {
		return State{}, err
	}
	defer unlock()

	if err := store.ensureOpen(); err != nil {
		return State{}, err
	}

	if err := ctx.Err(); err != nil {
		return State{}, err
	}

	state, err = store.writeStageRows(ctx, state, rows)
	if err != nil {
		return State{}, err
	}

	return state, nil
}

func prepareStage(
	ctx context.Context,
	generation Generation,
	coverage []CoverageRecord,
	chunks []Chunk,
) (State, []encodedRow, error) {
	orderedCoverage, orderedChunks, coverageHash, err := validateAndOrder(ctx, generation, coverage, chunks)
	if err != nil {
		return State{}, nil, err
	}

	state := State{
		SchemaVersion:       schemaVersion,
		Scope:               generation.Scope,
		BuildID:             generation.BuildID,
		LexicalGenerationID: generation.LexicalGenerationID,
		LexicalWatermark:    generation.LexicalWatermark,
		ModelSpace:          generation.ModelSpace,
		Dimensions:          generation.Dimensions,
		Status:              StatusReady,
		ChunkCount:          uint32(len(orderedChunks)),
		RecordCount:         uint32(len(orderedCoverage)),
		CoverageHash:        coverageHash,
		Omitted:             generation.Omitted,
		ReadyAt:             generation.ReadyAt.UTC(),
	}

	rows := make([]encodedRow, len(orderedChunks))
	encodedBytes := uint64(0)

	for index, chunk := range orderedChunks {
		if err := ctx.Err(); err != nil {
			return State{}, nil, err
		}

		value, encodeErr := encodeChunk(chunk)
		if encodeErr != nil {
			return State{}, nil, encodeErr
		}

		if len(value) > maxChunkValueBytes {
			return State{}, nil, ErrCapacity
		}

		rows[index] = encodedRow{key: chunkKey(generation.Scope, generation.BuildID, uint32(index)), value: value}

		encodedBytes += uint64(len(rows[index].key) + len(value))
		if encodedBytes > uint64(semanticcore.ProjectionByteLimit) {
			return State{}, nil, ErrCapacity
		}
	}

	state, err = finalizeEncodedBytes(state, encodedBytes)
	if err != nil {
		return State{}, nil, err
	}

	return state, rows, nil
}

//nolint:cyclop // Each independent bound must be checked before cloning caller-owned data.
func preflightStageInputs(
	ctx context.Context,
	generation Generation,
	coverage []CoverageRecord,
	chunks []Chunk,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if len(coverage) > semanticcore.ProjectionChunkLimit || len(chunks) > semanticcore.ProjectionChunkLimit {
		return ErrCapacity
	}

	if len(coverage) > len(chunks) {
		return ErrInvalid
	}

	coveredChunks := uint64(0)

	for _, record := range coverage {
		if err := ctx.Err(); err != nil {
			return err
		}

		if len(record.ItemID) > maxIdentityBytes || len(record.RevisionID) > maxIdentityBytes ||
			len(record.ContentHash) > 64 {
			return ErrInvalid
		}

		coveredChunks += uint64(record.ChunkCount)
		if coveredChunks > semanticcore.ProjectionChunkLimit {
			return ErrCapacity
		}

		if coveredChunks > uint64(len(chunks)) {
			return ErrInvalid
		}
	}

	if coveredChunks != uint64(len(chunks)) {
		return ErrInvalid
	}

	for _, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return err
		}

		if !chunkWithinPreflightBounds(generation, chunk) {
			return ErrInvalid
		}
	}

	return nil
}

//nolint:cyclop // This is an exhaustive declarative bound check over the encoded record.
func chunkWithinPreflightBounds(generation Generation, chunk Chunk) bool {
	if len(chunk.Scope.Key) > maxScopeKeyBytes || len(chunk.BuildID) > maxIdentityBytes ||
		len(chunk.LexicalGenerationID) > maxIdentityBytes || len(chunk.ModelSpace) > 64 ||
		len(chunk.ItemID) > maxIdentityBytes || len(chunk.RevisionID) > maxIdentityBytes ||
		len(chunk.ContentHash) > 64 || len(chunk.InputHash) > 64 ||
		len(chunk.Category) > maxIdentityBytes || len(chunk.MemoryKey) > maxIdentityBytes ||
		len(chunk.SourceIDs) > maxMetadataIDs || len(chunk.ConversationIDs) > maxMetadataIDs ||
		len(chunk.Vector) != generation.Dimensions {
		return false
	}

	for _, values := range [][]string{chunk.SourceIDs, chunk.ConversationIDs} {
		for _, value := range values {
			if len(value) > maxIdentityBytes {
				return false
			}
		}
	}

	return true
}

func (store *Store) writeStageRows(ctx context.Context, state State, rows []encodedRow) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}

	claimed, err := store.claimGeneration(state)
	if err != nil {
		return State{}, err
	}

	state = claimed

	for start := 0; start < len(rows); {
		if err := ctx.Err(); err != nil {
			return State{}, err
		}

		end := stageBatchEnd(rows, start)
		if err := store.writeRowBatch(rows[start:end]); err != nil {
			return State{}, err
		}

		start = end
		if store.afterStageBatch != nil {
			if err := store.afterStageBatch(start); err != nil {
				return State{}, err
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return State{}, err
	}

	if err := store.writeStoredState(state, true); err != nil {
		return State{}, err
	}

	return state, nil
}

func (store *Store) claimGeneration(state State) (State, error) {
	value, err := encodeStoredState(storedState{SchemaVersion: schemaVersion, Complete: false, State: state})
	if err != nil {
		return State{}, err
	}

	if err := store.db.Update(func(transaction *badger.Txn) error {
		if _, getErr := transaction.Get(stateKey(state.Scope, state.BuildID)); getErr != badger.ErrKeyNotFound {
			if getErr == nil {
				return ErrConflict
			}

			return getErr
		}

		return transaction.Set(stateKey(state.Scope, state.BuildID), value)
	}); err != nil {
		return State{}, fmt.Errorf("claim staged semantic generation: %w", err)
	}

	return state, nil
}

func stageBatchEnd(rows []encodedRow, start int) int {
	end := start

	batchBytes := 0
	for end < len(rows) && (end == start || batchBytes+len(rows[end].key)+len(rows[end].value) <= stageBatchBytes) {
		batchBytes += len(rows[end].key) + len(rows[end].value)
		end++
	}

	return end
}

func (store *Store) writeRowBatch(rows []encodedRow) error {
	if err := store.db.Update(func(transaction *badger.Txn) error {
		for _, row := range rows {
			if err := transaction.Set(row.key, row.value); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("write staged semantic chunks: %w", err)
	}

	return nil
}

func (store *Store) writeStoredState(state State, complete bool) error {
	value, err := encodeStoredState(storedState{SchemaVersion: schemaVersion, Complete: complete, State: state})
	if err != nil {
		return err
	}

	if err := store.db.Update(func(transaction *badger.Txn) error {
		return transaction.Set(stateKey(state.Scope, state.BuildID), value)
	}); err != nil {
		return fmt.Errorf("write staged semantic state: %w", err)
	}

	return nil
}
