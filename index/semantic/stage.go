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

// PreflightStage preserves the legacy portable admission contract.
func PreflightStage(ctx context.Context, generation Generation,
	coverage []CoverageRecord, chunks []Chunk) (State, error) {
	return semanticcore.PreflightStage(ctx, generation, coverage, chunks)
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

func prepareStage(ctx context.Context, generation Generation,
	coverage []CoverageRecord, chunks []Chunk) (State, []encodedRow, error) {
	state, ordered, err := semanticcore.PrepareProjection(ctx, generation, coverage, chunks)
	if err != nil {
		return State{}, nil, err
	}

	rows := make([]encodedRow, len(ordered))
	for index, chunk := range ordered {
		if err := ctx.Err(); err != nil {
			return State{}, nil, err
		}

		value, err := encodeChunk(chunk)
		if err != nil {
			return State{}, nil, err
		}

		rows[index] = encodedRow{key: chunkKey(generation.Scope, generation.BuildID, uint32(index)), value: value}
	}

	return state, rows, nil
}

func preflightStageInputs(ctx context.Context, generation Generation, coverage []CoverageRecord, chunks []Chunk) error {
	return semanticcore.PreflightStageInputs(ctx, generation, coverage, chunks)
}

func chunkWithinPreflightBounds(generation Generation, chunk Chunk) bool {
	return semanticcore.ChunkWithinPreflightBounds(generation, chunk)
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
