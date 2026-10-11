package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// ApplyCanonicalMutation appends immutable records and advances the exact
// scope cursor in one Badger transaction.
func (s *CanonicalStore) ApplyCanonicalMutation(
	ctx context.Context,
	mutation memory.CanonicalMutation,
) (memory.CanonicalMutationOutcome, error) {
	if err := canonicalContextError(ctx); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	if err := mutation.Validate(); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	if s == nil {
		return memory.CanonicalMutationOutcome{}, memory.PermanentError(
			memory.CodeStoreFailure,
			"canonical badger store is closed",
			nil,
		)
	}

	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()

	if s.db == nil {
		return memory.CanonicalMutationOutcome{}, memory.PermanentError(
			memory.CodeStoreFailure,
			"canonical badger store is closed",
			nil,
		)
	}

	scopeKey, err := badgerScopeKey(mutation.Scope)
	if err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	operationKey, err := badgerOperationKey(mutation.Scope, mutation.Operation.OperationID)
	if err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	var outcome memory.CanonicalMutationOutcome

	err = s.db.Update(func(txn *badger.Txn) error {
		next, err := s.applyCanonicalMutationTxn(txn, mutation, scopeKey, operationKey)
		if err != nil {
			return err
		}

		outcome = next

		return nil
	})
	if err != nil {
		return memory.CanonicalMutationOutcome{}, canonicalStoreError("apply canonical mutation", err)
	}

	return outcome, nil
}
