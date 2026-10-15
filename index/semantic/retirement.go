package semantic

import (
	"bytes"
	"context"

	"github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// retireActiveGeneration reclaims only the previously published generation in
// the pointer-swap transaction. loadStoredState bounds the work to at most 8192
// fixed-size chunk keys plus one corruption check; no chunk values, staging,
// or other scopes are scanned.
// Snapshots are independent copies, and existing Badger readers retain their
// MVCC view. Cancellation rolls back both retirement and publication, leaving
// no cleanup queue or partially retired generation to recover.
func retireActiveGeneration(
	ctx context.Context,
	transaction *badger.Txn,
	scope memory.Scope,
	current activePointer,
	exists bool,
) error {
	if !exists {
		return ctx.Err()
	}

	record, err := loadStoredState(transaction, scope, current.BuildID)
	if err != nil {
		return err
	}

	if !record.Complete || record.State.PublicationEpoch != current.Epoch {
		return ErrCorrupt
	}

	if err := validateRetirementState(record); err != nil {
		return err
	}

	if err := retireGenerationChunks(ctx, transaction, record.State); err != nil {
		return err
	}

	return transaction.Delete(stateKey(scope, current.BuildID))
}

func validateRetirementState(record storedState) error {
	switch record.State.Status {
	case StatusReady:
		if validateReadyState(record) != nil {
			return ErrCorrupt
		}

		return nil
	case StatusDegraded:
		return validateDegradedState(record)
	default:
		return ErrCorrupt
	}
}

func retireGenerationChunks(ctx context.Context, transaction *badger.Txn, state State) error {
	prefix := chunkPrefix(state.Scope, state.BuildID)
	options := badger.DefaultIteratorOptions
	options.Prefix = prefix
	options.PrefetchValues = false

	iterator := transaction.NewIterator(options)
	defer iterator.Close()

	var count uint32

	for iterator.Seek(prefix); iterator.ValidForPrefix(prefix); iterator.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}

		key := iterator.Item().KeyCopy(nil)
		if count >= state.ChunkCount || !bytes.Equal(key, chunkKey(state.Scope, state.BuildID, count)) {
			return ErrCorrupt
		}

		if err := transaction.Delete(key); err != nil {
			return err
		}

		count++
	}

	if count != state.ChunkCount {
		return ErrCorrupt
	}

	return ctx.Err()
}
