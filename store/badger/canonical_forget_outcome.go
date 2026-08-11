package badger

import (
	"context"
	"errors"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func (s *CanonicalStore) LoadCanonicalForgetOutcome(ctx context.Context, scope memory.Scope, operationID string, kind memory.ForgetKind) (memory.ForgetOutcome, bool, error) {
	if err := canonicalContextError(ctx); err != nil {
		return memory.ForgetOutcome{}, false, err
	}
	if err := scope.Validate(); err != nil {
		return memory.ForgetOutcome{}, false, err
	}
	if err := kind.Validate(); err != nil || strings.TrimSpace(operationID) == "" || strings.TrimSpace(operationID) != operationID {
		return memory.ForgetOutcome{}, false, memory.PermanentError(memory.CodeInvalidDerived, "canonical forget outcome lookup is invalid", err)
	}
	if s == nil {
		return memory.ForgetOutcome{}, false, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return memory.ForgetOutcome{}, false, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	key, err := badgerForgetOutcomeKey(scope, kind, operationID)
	if err != nil {
		return memory.ForgetOutcome{}, false, err
	}
	var stored badgerCanonicalForgetOutcome
	err = s.db.View(func(txn *badger.Txn) error {
		return getCanonicalRecord(txn, key, badgerRecordForgetOutcome, &stored)
	})
	if errors.Is(err, badger.ErrKeyNotFound) {
		return memory.ForgetOutcome{}, false, nil
	}
	if err != nil {
		return memory.ForgetOutcome{}, false, canonicalStoreError("load canonical forget outcome", err)
	}
	if err := stored.Outcome.Validate(); err != nil || stored.Outcome.Scope != scope || stored.Outcome.OperationID != operationID || stored.Outcome.Kind != kind {
		return memory.ForgetOutcome{}, false, memory.PermanentError(memory.CodeStoreFailure, "stored canonical forget outcome is invalid", err)
	}
	return stored.Outcome, true, nil
}
