package semantic

import (
	"context"
	"encoding/json"

	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/dgraph-io/badger/v4"
)

// LoadReusable returns owned vector material for builds only. Snapshot applies
// the complete model, coverage, vector, and logical encoded-budget validation.
func (store *Store) LoadReusable(ctx context.Context, view View) (semanticcore.Reusable, bool, error) {
	snapshot, found, err := store.Snapshot(ctx, view)
	if err != nil || !found {
		return semanticcore.Reusable{}, false, err
	}

	result := semanticcore.Reusable{
		State: snapshot.state, Chunks: make([]semanticcore.ReusableChunk, len(snapshot.chunks)),
	}
	for index, chunk := range snapshot.chunks {
		if err := ctx.Err(); err != nil {
			return semanticcore.Reusable{}, false, err
		}

		result.Chunks[index] = semanticcore.ReusableChunk{ItemID: chunk.ItemID, RevisionID: chunk.RevisionID,
			Revision: chunk.Revision, ContentHash: chunk.ContentHash, InputHash: chunk.InputHash,
			Ordinal: chunk.Ordinal, Vector: append([]float32(nil), chunk.Vector...)}
	}

	return result, true, nil
}

// ObservedMetadataReady probes only metadata in a compatible ready generation.
// It never materializes a float vector or a generation-sized chunk slice.
//
//nolint:cyclop // Exact view and publication gates remain explicit.
func (store *Store) ObservedMetadataReady(ctx context.Context, view View) (bool, error) {
	if ctx == nil || validateView(view) != nil {
		return false, ErrInvalid
	}

	unlock, err := store.gate.acquire(ctx, false)
	if err != nil {
		return false, err
	}
	defer unlock()

	if err := store.ensureOpen(); err != nil {
		return false, err
	}

	ready := false
	err = store.db.View(func(transaction *badger.Txn) error {
		pointer, found, err := loadPointer(transaction, view.Scope)
		if err != nil || !found {
			return err
		}

		record, err := loadStoredState(transaction, view.Scope, pointer.BuildID)
		if err != nil {
			return err
		}

		if record.State.Status != StatusReady || !stateMatchesView(record.State, view) {
			return nil
		}

		if record.State.PublicationEpoch != pointer.Epoch || validateReadyState(record) != nil {
			return ErrCorrupt
		}

		ready, err = probeObservedMetadata(ctx, transaction, record.State)

		return err
	})

	return ready, err
}

//nolint:cyclop // Streaming budget, cancellation, and metadata checks remain adjacent.
func probeObservedMetadata(ctx context.Context, transaction *badger.Txn, state State) (bool, error) {
	remaining, err := generationRowBudget(state)
	if err != nil {
		return false, err
	}

	iterator := transaction.NewIterator(badger.DefaultIteratorOptions)
	defer iterator.Close()

	prefix := chunkPrefix(state.Scope, state.BuildID)
	count := uint32(0)
	ready := true

	for iterator.Seek(prefix); iterator.ValidForPrefix(prefix); iterator.Next() {
		if err := ctx.Err(); err != nil {
			return false, err
		}

		item := iterator.Item()

		keyBytes := uint64(len(item.Key()))
		if keyBytes > remaining || count >= state.ChunkCount {
			return false, ErrCorrupt
		}

		value, err := readItemValueBounded(item, min(maxChunkValueBytes, int(remaining-keyBytes)))
		if err != nil {
			return false, err
		}

		var metadata struct {
			ObservedAt string `json:"observed_at"`
		}
		if err := json.Unmarshal(value, &metadata); err != nil {
			return false, ErrCorrupt
		}

		observed, err := parseOptionalTime(metadata.ObservedAt)
		if err != nil {
			return false, err
		}

		ready = ready && observed != nil && !observed.IsZero()
		remaining -= keyBytes + uint64(len(value))
		count++
	}

	if remaining != 0 || count != state.ChunkCount {
		return false, ErrCorrupt
	}

	return ready, ctx.Err()
}
