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

func (s *CanonicalStore) CommitCanonicalForget(ctx context.Context, request memory.CanonicalForgetCommitRequest) (memory.ForgetOutcome, error) {
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
		var existing badgerCanonicalForgetOutcome
		readErr := getCanonicalRecord(txn, key, badgerRecordForgetOutcome, &existing)
		if readErr == nil {
			if existing.Fingerprint != request.Fingerprint {
				return memory.PermanentError(memory.CodeConflict, "canonical forget operation identity was reused", nil)
			}
			if err := existing.Outcome.Validate(); err != nil || existing.Outcome.Scope != request.Scope || existing.Outcome.OperationID != request.OperationID || existing.Outcome.Kind != request.Kind {
				return memory.PermanentError(memory.CodeStoreFailure, "stored canonical forget outcome is invalid", err)
			}
			outcome = existing.Outcome
			return nil
		}
		if !errors.Is(readErr, badger.ErrKeyNotFound) {
			return readErr
		}
		state := memory.ScopeState{SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: request.Scope}
		stateErr := getCanonicalRecord(txn, scopeKey, badgerRecordScope, &state)
		if stateErr != nil && !errors.Is(stateErr, badger.ErrKeyNotFound) {
			return stateErr
		}
		if err := state.Validate(); err != nil {
			return err
		}
		if state.Version != request.ExpectedScopeVersion {
			return memory.RetryableError(memory.CodeConflict, "canonical forget scope version changed", nil)
		}
		state.Version++
		state.ChangeSeq++
		outcome = memory.ForgetOutcome{
			SchemaVersion: memory.DerivedSchemaVersionV1,
			OperationID:   request.OperationID,
			Kind:          request.Kind,
			Scope:         request.Scope,
			ScopeVersion:  state.Version,
			Sources:       append([]memory.SourceRef(nil), request.Sources...),
			Revisions:     append([]memory.RevisionRef(nil), request.Revisions...),
		}
		if err := outcome.Validate(); err != nil {
			return err
		}
		changeKey, keyErr := badgerScopeChangeKey(request.Scope, state.ChangeSeq)
		if keyErr != nil {
			return keyErr
		}
		change := memory.ScopeChange{
			Sequence:    state.ChangeSeq,
			OperationID: request.OperationID,
			OccurredAt:  request.ForgottenAt.UTC(),
		}
		for _, revision := range request.Revisions {
			change.RevisionIDs = append(change.RevisionIDs, revision.RevisionID)
		}
		if err := putCanonicalRecord(txn, changeKey, badgerRecordChange, change); err != nil {
			return err
		}
		if err := putCanonicalRecord(txn, scopeKey, badgerRecordScope, state); err != nil {
			return err
		}
		if err := putCanonicalRecord(txn, key, badgerRecordForgetOutcome, badgerCanonicalForgetOutcome{
			Fingerprint: request.Fingerprint,
			Outcome:     outcome,
		}); err != nil {
			return err
		}
		if s.beforeCanonicalMutationCommit != nil {
			return s.beforeCanonicalMutationCommit()
		}
		return nil
	})
	if err != nil {
		return memory.ForgetOutcome{}, canonicalStoreError("commit canonical forget outcome", err)
	}
	return outcome, nil
}
