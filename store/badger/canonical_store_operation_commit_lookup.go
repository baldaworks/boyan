package badger

import (
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func loadCanonicalOperationOutcome(
	txn *badger.Txn,
	request memory.CanonicalOperationCommitRequest,
	operationKey []byte,
) (memory.CanonicalMutationOutcome, bool, error) {
	var stored badgerCanonicalOperation
	err := getCanonicalRecord(txn, operationKey, badgerRecordOperation, &stored)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return memory.CanonicalMutationOutcome{}, false, nil
	}
	if err != nil {
		return memory.CanonicalMutationOutcome{}, false, err
	}
	if stored.Fingerprint != request.Fingerprint {
		return memory.CanonicalMutationOutcome{}, true, memory.PermanentError(memory.CodeConflict, "canonical operation identity was reused", nil)
	}
	if err := stored.Outcome.Validate(); err != nil {
		return memory.CanonicalMutationOutcome{}, true, err
	}
	return stored.Outcome, true, nil
}

func loadCanonicalOperationScopeState(
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
