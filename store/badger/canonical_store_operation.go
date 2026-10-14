package badger

import (
	"context"
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

type badgerCanonicalOperation struct {
	Fingerprint string                          `json:"fingerprint"`
	Outcome     memory.CanonicalMutationOutcome `json:"outcome"`
}

// LoadCanonicalOperation reads one exact-scope canonical mutation replay record.
func (s *CanonicalStore) LoadCanonicalOperation(
	ctx context.Context,
	scope memory.Scope,
	operationID string,
) (memory.CanonicalOperationRecord, bool, error) {
	if err := canonicalContextError(ctx); err != nil {
		return memory.CanonicalOperationRecord{}, false, err
	}

	if err := scope.Validate(); err != nil {
		return memory.CanonicalOperationRecord{}, false, err
	}

	if err := (memory.RevisionRef{ItemID: operationID, RevisionID: operationID}).Validate(); err != nil {
		return memory.CanonicalOperationRecord{}, false, memory.PermanentError(
			memory.CodeInvalidDerived,
			"canonical operation id is invalid",
			nil,
		)
	}

	if s == nil {
		return memory.CanonicalOperationRecord{}, false, memory.PermanentError(
			memory.CodeStoreFailure,
			"canonical badger store is closed",
			nil,
		)
	}

	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()

	if s.db == nil {
		return memory.CanonicalOperationRecord{}, false, memory.PermanentError(
			memory.CodeStoreFailure,
			"canonical badger store is closed",
			nil,
		)
	}

	key, err := badgerOperationKey(scope, operationID)
	if err != nil {
		return memory.CanonicalOperationRecord{}, false, err
	}

	return loadBoundCanonicalOperation(s.db, key, scope, operationID)
}

func loadBoundCanonicalOperation(
	db *badger.DB,
	key []byte,
	scope memory.Scope,
	operationID string,
) (memory.CanonicalOperationRecord, bool, error) {
	var stored badgerCanonicalOperation

	operationFound := false

	err := db.View(func(txn *badger.Txn) error {
		if err := getCanonicalRecord(txn, key, badgerRecordOperation, &stored); err != nil {
			return err
		}

		operationFound = true

		if _, _, err := canonicalOperationRecord(stored); err != nil {
			return err
		}

		return validateCanonicalOutcomeBinding(txn, scope, operationID, stored.Outcome)
	})
	if errors.Is(err, badger.ErrKeyNotFound) && !operationFound {
		return memory.CanonicalOperationRecord{}, false, nil
	}

	if err != nil {
		return memory.CanonicalOperationRecord{}, false, canonicalStoreError("load canonical operation", err)
	}

	return canonicalOperationRecord(stored)
}

func canonicalOperationRecord(stored badgerCanonicalOperation) (memory.CanonicalOperationRecord, bool, error) {
	record := memory.CanonicalOperationRecord{Fingerprint: stored.Fingerprint, Outcome: stored.Outcome}
	if err := record.Validate(); err != nil {
		return memory.CanonicalOperationRecord{}, false, memory.PermanentError(
			memory.CodeStoreFailure,
			"stored canonical operation outcome is invalid",
			err,
		)
	}

	return record, true, nil
}
