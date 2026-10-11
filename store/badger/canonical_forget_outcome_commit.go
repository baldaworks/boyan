package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func (s *CanonicalStore) CommitCanonicalForget(
	ctx context.Context,
	request memory.CanonicalForgetCommitRequest,
) (memory.ForgetOutcome, error) {
	if err := canonicalContextError(ctx); err != nil {
		return memory.ForgetOutcome{}, err
	}

	if err := request.Validate(); err != nil {
		return memory.ForgetOutcome{}, err
	}

	if s == nil {
		return memory.ForgetOutcome{}, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}

	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()

	if s.db == nil {
		return memory.ForgetOutcome{}, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}

	key, err := badgerForgetOutcomeKey(request.Scope, request.Kind, request.OperationID)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}

	scopeKey, err := badgerScopeKey(request.Scope)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}

	var outcome memory.ForgetOutcome

	err = s.db.Update(func(txn *badger.Txn) error {
		next, err := s.commitCanonicalForgetOutcome(txn, request, key, scopeKey)
		if err != nil {
			return err
		}

		outcome = next

		return nil
	})
	if err != nil {
		return memory.ForgetOutcome{}, canonicalStoreError("commit canonical forget outcome", err)
	}

	return outcome, nil
}
