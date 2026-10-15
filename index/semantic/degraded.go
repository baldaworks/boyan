package semantic

import (
	"context"
	"fmt"

	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/dgraph-io/badger/v4"
)

// Degraded is a text-free failure marker for one exactly verified view.
type Degraded struct {
	Generation          Generation
	SafeReason          semanticcore.Reason
	ExpectedActiveEpoch uint64
}

// PublishDegraded atomically publishes a bounded marker instead of a partial
// generation. A degraded marker is diagnostic state and never a read snapshot.
func (store *Store) PublishDegraded(ctx context.Context, marker Degraded, expectedActiveBuildID string) (State, error) {
	if ctx == nil || validateDegraded(marker, expectedActiveBuildID) != nil {
		return State{}, ErrInvalid
	}

	if err := ctx.Err(); err != nil {
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

	var state State

	err = store.db.Update(func(transaction *badger.Txn) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		state, err = publishDegradedTransaction(transaction, marker, expectedActiveBuildID)

		return err
	})
	if err != nil {
		return State{}, fmt.Errorf("publish degraded semantic state: %w", err)
	}

	return state, nil
}

func validateDegraded(marker Degraded, expectedActiveBuildID string) error {
	if err := validateGeneration(marker.Generation); err != nil {
		return err
	}

	if !marker.SafeReason.Valid() {
		return ErrInvalid
	}

	if expectedActiveBuildID != "" && !validIdentity(expectedActiveBuildID) {
		return ErrInvalid
	}

	if (expectedActiveBuildID == "") != (marker.ExpectedActiveEpoch == 0) {
		return ErrInvalid
	}

	return nil
}

func encodeDegraded(marker Degraded, epoch uint64) (State, []byte, []byte, error) {
	state := State{
		SchemaVersion:       schemaVersion,
		Scope:               marker.Generation.Scope,
		BuildID:             marker.Generation.BuildID,
		LexicalGenerationID: marker.Generation.LexicalGenerationID,
		LexicalWatermark:    marker.Generation.LexicalWatermark,
		ModelSpace:          marker.Generation.ModelSpace,
		Dimensions:          marker.Generation.Dimensions,
		Status:              StatusDegraded,
		SafeReason:          string(marker.SafeReason),
		PublicationEpoch:    epoch,
		CoverageHash:        coverageFingerprint(nil),
		Omitted:             marker.Generation.Omitted,
		ReadyAt:             marker.Generation.ReadyAt.UTC(),
	}

	state, err := finalizeEncodedBytes(state, 0)
	if err != nil {
		return State{}, nil, nil, err
	}

	stateValue, err := encodeStoredState(storedState{SchemaVersion: schemaVersion, Complete: true, State: state})
	if err != nil {
		return State{}, nil, nil, err
	}

	pointerValue, err := encodeActivePointer(activePointer{
		SchemaVersion: schemaVersion,
		ScopeKey:      state.Scope.Key,
		ScopeKind:     string(state.Scope.Kind),
		BuildID:       state.BuildID,
		Epoch:         state.PublicationEpoch,
	})
	if err != nil {
		return State{}, nil, nil, err
	}

	return state, stateValue, pointerValue, nil
}

func publishDegradedTransaction(
	transaction *badger.Txn,
	marker Degraded,
	expectedActiveBuildID string,
) (State, error) {
	current, exists, err := loadPointer(transaction, marker.Generation.Scope)
	if err != nil {
		return State{}, err
	}

	if !pointerMatchesExpected(current, exists, expectedActiveBuildID, marker.ExpectedActiveEpoch) {
		return State{}, ErrConflict
	}

	epoch, err := nextPublicationEpoch(current, exists)
	if err != nil {
		return State{}, err
	}

	state, stateValue, pointerValue, err := encodeDegraded(marker, epoch)
	if err != nil {
		return State{}, err
	}

	if _, err := transaction.Get(stateKey(state.Scope, state.BuildID)); err != badger.ErrKeyNotFound {
		if err == nil {
			return State{}, ErrConflict
		}

		return State{}, err
	}

	if err := transaction.Set(stateKey(state.Scope, state.BuildID), stateValue); err != nil {
		return State{}, err
	}

	if err := transaction.Set(activeKey(state.Scope), pointerValue); err != nil {
		return State{}, err
	}

	return state, nil
}
