package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// CommitCanonicalOperation durably advances one scope for an operation that
// has no revision records while preserving replay and CAS semantics.
func (s *CanonicalStore) CommitCanonicalOperation(
	ctx context.Context,
	request memory.CanonicalOperationCommitRequest,
) (memory.CanonicalMutationOutcome, error) {
	if err := canonicalContextError(ctx); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	request.BoundaryIdentity = cloneBoundaryIdentity(request.BoundaryIdentity)

	if err := request.Validate(); err != nil {
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

	operationKey, err := badgerOperationKey(request.Scope, request.OperationID)
	if err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	scopeKey, err := badgerScopeKey(request.Scope)
	if err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	var outcome memory.CanonicalMutationOutcome

	err = s.db.Update(func(txn *badger.Txn) error {
		var txErr error

		outcome, txErr = s.commitCanonicalOperationTxn(txn, request, scopeKey, operationKey)

		return txErr
	})
	if err != nil {
		err = boundaryCertificateWriteError(s.db, request.BoundaryIdentity, err)

		return memory.CanonicalMutationOutcome{}, canonicalStoreError("commit canonical operation", err)
	}

	return outcome, nil
}
