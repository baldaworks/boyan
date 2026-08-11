package badger

import (
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func loadCanonicalForgetOutcome(
	txn *badger.Txn,
	request memory.CanonicalForgetCommitRequest,
	key []byte,
) (memory.ForgetOutcome, bool, error) {
	var existing badgerCanonicalForgetOutcome
	readErr := getCanonicalRecord(txn, key, badgerRecordForgetOutcome, &existing)
	if errors.Is(readErr, badger.ErrKeyNotFound) {
		return memory.ForgetOutcome{}, false, nil
	}
	if readErr != nil {
		return memory.ForgetOutcome{}, false, readErr
	}
	if existing.Fingerprint != request.Fingerprint {
		return memory.ForgetOutcome{}, true, memory.PermanentError(memory.CodeConflict, "canonical forget operation identity was reused", nil)
	}
	if err := existing.Outcome.Validate(); err != nil ||
		existing.Outcome.Scope != request.Scope ||
		existing.Outcome.OperationID != request.OperationID ||
		existing.Outcome.Kind != request.Kind {
		return memory.ForgetOutcome{}, true, memory.PermanentError(memory.CodeStoreFailure, "stored canonical forget outcome is invalid", err)
	}
	return existing.Outcome, true, nil
}

func loadCanonicalForgetScopeState(txn *badger.Txn, scope memory.Scope, scopeKey []byte) (memory.ScopeState, error) {
	state := memory.ScopeState{SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: scope}
	readErr := getCanonicalRecord(txn, scopeKey, badgerRecordScope, &state)
	if readErr != nil && !errors.Is(readErr, badger.ErrKeyNotFound) {
		return memory.ScopeState{}, readErr
	}
	if err := state.Validate(); err != nil || state.Scope != scope {
		return memory.ScopeState{}, memory.PermanentError(memory.CodeStoreFailure, "stored canonical forget scope state is invalid", err)
	}
	return state, nil
}
