package badger

import (
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func loadCanonicalMutationOutcome(
	txn *badger.Txn,
	mutation memory.CanonicalMutation,
	operationKey []byte,
) (memory.CanonicalMutationOutcome, bool, error) {
	var operation badgerCanonicalOperation
	err := getCanonicalRecord(txn, operationKey, badgerRecordOperation, &operation)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return memory.CanonicalMutationOutcome{}, false, nil
	}
	if err != nil {
		return memory.CanonicalMutationOutcome{}, false, err
	}
	if operation.Fingerprint != mutation.Operation.Fingerprint {
		return memory.CanonicalMutationOutcome{}, true, memory.PermanentError(memory.CodeConflict, "canonical operation identity was reused", nil)
	}
	if err := operation.Outcome.Validate(); err != nil {
		return memory.CanonicalMutationOutcome{}, true, err
	}
	return operation.Outcome, true, nil
}

func loadCanonicalMutationScopeState(
	txn *badger.Txn,
	scope memory.Scope,
	scopeKey []byte,
) (memory.ScopeState, error) {
	state := memory.ScopeState{SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: scope}
	err := getCanonicalRecord(txn, scopeKey, badgerRecordScope, &state)
	if err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
		return memory.ScopeState{}, err
	}
	if err := state.Validate(); err != nil || state.Scope != scope {
		return memory.ScopeState{}, memory.PermanentError(memory.CodeStoreFailure, "stored canonical scope state is invalid", err)
	}
	return state, nil
}
