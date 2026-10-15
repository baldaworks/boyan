package semantic

import (
	"context"
	"fmt"

	"github.com/dgraph-io/badger/v4"
)

// Publish validates a complete staged generation, then atomically compares
// and replaces the scope's active pointer.
func (store *Store) Publish(ctx context.Context, publication Publication) error {
	if ctx == nil || validatePublication(publication) != nil {
		return ErrInvalid
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	unlock, err := store.gate.acquire(ctx, true)
	if err != nil {
		return err
	}
	defer unlock()

	if err := store.ensureOpen(); err != nil {
		return err
	}

	err = store.db.View(func(transaction *badger.Txn) error {
		return validatePublicationCandidate(ctx, transaction, publication)
	})
	if err != nil {
		return fmt.Errorf("validate semantic publication: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	err = store.db.Update(func(transaction *badger.Txn) error {
		return swapPublication(ctx, transaction, publication)
	})
	if err != nil {
		return fmt.Errorf("publish semantic generation: %w", err)
	}

	return nil
}

func validatePublication(publication Publication) error {
	if err := validateView(publication.View); err != nil {
		return err
	}

	if !validIdentity(publication.BuildID) {
		return ErrInvalid
	}

	if publication.ExpectedActiveBuildID != "" && !validIdentity(publication.ExpectedActiveBuildID) {
		return ErrInvalid
	}

	if (publication.ExpectedActiveBuildID == "") != (publication.ExpectedActiveEpoch == 0) {
		return ErrInvalid
	}

	return nil
}

func validatePublicationCandidate(ctx context.Context, transaction *badger.Txn, publication Publication) error {
	record, err := loadStoredState(transaction, publication.Scope, publication.BuildID)
	if err != nil {
		return err
	}

	if !stateMatchesView(record.State, publication.View) {
		return ErrConflict
	}

	_, err = loadValidatedGeneration(ctx, transaction, record)

	return err
}

//nolint:cyclop,funlen // The atomic CAS transaction keeps every validation adjacent to its write.
func swapPublication(ctx context.Context, transaction *badger.Txn, publication Publication) error {
	current, exists, err := loadPointer(transaction, publication.Scope)
	if err != nil {
		return err
	}

	if !pointerMatchesExpected(current, exists, publication.ExpectedActiveBuildID, publication.ExpectedActiveEpoch) {
		return ErrConflict
	}

	record, err := loadStoredState(transaction, publication.Scope, publication.BuildID)
	if err != nil {
		return err
	}

	if !record.Complete || record.State.Status != StatusReady ||
		!stateMatchesView(record.State, publication.View) {
		return ErrConflict
	}

	if record.State.PublicationEpoch != 0 {
		return ErrConflict
	}

	nextEpoch, err := nextPublicationEpoch(current, exists)
	if err != nil {
		return err
	}

	rowBytes, err := generationRowBudget(record.State)
	if err != nil {
		return err
	}

	state := record.State
	state.PublicationEpoch = nextEpoch

	state, err = finalizeEncodedBytes(state, rowBytes)
	if err != nil {
		return err
	}

	encoded, err := encodeActivePointer(activePointer{
		SchemaVersion: schemaVersion,
		ScopeKey:      publication.Scope.Key,
		ScopeKind:     string(publication.Scope.Kind),
		BuildID:       publication.BuildID,
		Epoch:         state.PublicationEpoch,
	})
	if err != nil {
		return err
	}

	stateValue, err := encodeStoredState(storedState{SchemaVersion: schemaVersion, Complete: true, State: state})
	if err != nil {
		return err
	}

	if err := transaction.Set(stateKey(state.Scope, state.BuildID), stateValue); err != nil {
		return err
	}

	if err := retireActiveGeneration(ctx, transaction, publication.Scope, current, exists); err != nil {
		return err
	}

	return transaction.Set(activeKey(publication.Scope), encoded)
}

func pointerMatchesExpected(pointer activePointer, exists bool, buildID string, epoch uint64) bool {
	if !exists {
		return buildID == "" && epoch == 0
	}

	return pointer.BuildID == buildID && pointer.Epoch == epoch
}

func nextPublicationEpoch(pointer activePointer, exists bool) (uint64, error) {
	if !exists {
		return 1, nil
	}

	if pointer.Epoch == ^uint64(0) {
		return 0, ErrCapacity
	}

	return pointer.Epoch + 1, nil
}
