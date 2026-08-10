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
func (s *CanonicalStore) LoadCanonicalOperation(ctx context.Context, scope memory.Scope, operationID string) (memory.CanonicalOperationRecord, bool, error) {
	if err := canonicalContextError(ctx); err != nil {
		return memory.CanonicalOperationRecord{}, false, err
	}
	if err := scope.Validate(); err != nil {
		return memory.CanonicalOperationRecord{}, false, err
	}
	if err := (memory.RevisionRef{ItemID: operationID, RevisionID: operationID}).Validate(); err != nil {
		return memory.CanonicalOperationRecord{}, false, memory.PermanentError(memory.CodeInvalidDerived, "canonical operation id is invalid", nil)
	}
	if s == nil {
		return memory.CanonicalOperationRecord{}, false, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return memory.CanonicalOperationRecord{}, false, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	key, err := badgerOperationKey(scope, operationID)
	if err != nil {
		return memory.CanonicalOperationRecord{}, false, err
	}
	var stored badgerCanonicalOperation
	err = s.db.View(func(txn *badger.Txn) error {
		return getCanonicalRecord(txn, key, badgerRecordOperation, &stored)
	})
	if errors.Is(err, badger.ErrKeyNotFound) {
		return memory.CanonicalOperationRecord{}, false, nil
	}
	if err != nil {
		return memory.CanonicalOperationRecord{}, false, canonicalStoreError("load canonical operation", err)
	}
	record := memory.CanonicalOperationRecord{Fingerprint: stored.Fingerprint, Outcome: stored.Outcome}
	if err := record.Validate(); err != nil {
		return memory.CanonicalOperationRecord{}, false, memory.PermanentError(memory.CodeStoreFailure, "stored canonical operation outcome is invalid", err)
	}
	return record, true, nil
}

// CommitCanonicalOperation durably advances one scope for an operation that
// has no revision records while preserving replay and CAS semantics.
func (s *CanonicalStore) CommitCanonicalOperation(ctx context.Context, request memory.CanonicalOperationCommitRequest) (memory.CanonicalMutationOutcome, error) {
	if err := canonicalContextError(ctx); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}
	if err := request.Validate(); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}
	if s == nil {
		return memory.CanonicalMutationOutcome{}, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return memory.CanonicalMutationOutcome{}, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
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
		var stored badgerCanonicalOperation
		lookupErr := getCanonicalRecord(txn, operationKey, badgerRecordOperation, &stored)
		if lookupErr == nil {
			if stored.Fingerprint != request.Fingerprint {
				return memory.PermanentError(memory.CodeConflict, "canonical operation identity was reused", nil)
			}
			if err := stored.Outcome.Validate(); err != nil {
				return err
			}
			outcome = stored.Outcome
			return nil
		}
		if !errors.Is(lookupErr, badger.ErrKeyNotFound) {
			return lookupErr
		}
		state := memory.ScopeState{SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: request.Scope}
		stateErr := getCanonicalRecord(txn, scopeKey, badgerRecordScope, &state)
		if stateErr != nil && !errors.Is(stateErr, badger.ErrKeyNotFound) {
			return stateErr
		}
		if err := state.Validate(); err != nil || state.Scope != request.Scope {
			return memory.PermanentError(memory.CodeStoreFailure, "stored canonical scope state is invalid", err)
		}
		if state.Version != request.ExpectedScopeVersion {
			return memory.PermanentError(memory.CodeConflict, "canonical scope version changed", nil)
		}
		state.Version++
		state.ChangeSeq++
		outcome = memory.CanonicalMutationOutcome{ScopeVersion: state.Version, ChangeSeq: state.ChangeSeq}
		if err := outcome.Validate(); err != nil {
			return err
		}
		changeKey, keyErr := badgerScopeChangeKey(request.Scope, state.ChangeSeq)
		if keyErr != nil {
			return keyErr
		}
		change := memory.ScopeChange{Sequence: state.ChangeSeq, OperationID: request.OperationID, OccurredAt: request.CommittedAt}
		if err := putCanonicalRecord(txn, changeKey, badgerRecordChange, change); err != nil {
			return err
		}
		if err := putCanonicalRecord(txn, scopeKey, badgerRecordScope, state); err != nil {
			return err
		}
		if err := putCanonicalRecord(txn, operationKey, badgerRecordOperation, badgerCanonicalOperation{Fingerprint: request.Fingerprint, Outcome: outcome}); err != nil {
			return err
		}
		if s.beforeCanonicalMutationCommit != nil {
			return s.beforeCanonicalMutationCommit()
		}
		return nil
	})
	if err != nil {
		return memory.CanonicalMutationOutcome{}, canonicalStoreError("commit canonical operation", err)
	}
	return outcome, nil
}

// LoadScopeState reads the small mutable cursor for one exact scope.
func (s *CanonicalStore) LoadScopeState(ctx context.Context, scope memory.Scope) (memory.ScopeState, error) {
	if err := canonicalContextError(ctx); err != nil {
		return memory.ScopeState{}, err
	}
	if err := scope.Validate(); err != nil {
		return memory.ScopeState{}, err
	}
	if s == nil {
		return memory.ScopeState{}, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return memory.ScopeState{}, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	key, err := badgerScopeKey(scope)
	if err != nil {
		return memory.ScopeState{}, err
	}
	state := memory.ScopeState{SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: scope}
	err = s.db.View(func(txn *badger.Txn) error {
		err := getCanonicalRecord(txn, key, badgerRecordScope, &state)
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		}
		return err
	})
	if err != nil {
		return memory.ScopeState{}, canonicalStoreError("load canonical scope state", err)
	}
	if err := state.Validate(); err != nil || state.Scope != scope {
		return memory.ScopeState{}, memory.PermanentError(memory.CodeStoreFailure, "stored canonical scope state is invalid", err)
	}
	return state, nil
}
