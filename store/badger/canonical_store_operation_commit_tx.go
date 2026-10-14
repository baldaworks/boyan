package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func (s *CanonicalStore) commitCanonicalOperationTxn(
	txn *badger.Txn,
	request memory.CanonicalOperationCommitRequest,
	scopeKey []byte,
	operationKey []byte,
) (memory.CanonicalMutationOutcome, error) {
	if err := checkCanonicalWriterFence(txn, request.Scope); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	if err := prepareBoundaryIdentity(
		txn, request.Scope, request.BoundaryIdentity, request.OperationID, operationKey,
	); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	if outcome, ok, err := loadCanonicalOperationOutcome(txn, request, operationKey); ok || err != nil {
		return outcome, err
	}

	state, err := loadExpectedCanonicalOperationState(txn, request, scopeKey)
	if err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	if err := persistBoundaryCertificate(txn, request.Scope, request.BoundaryIdentity); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	state.Version++
	state.ChangeSeq++

	outcome := memory.CanonicalMutationOutcome{ScopeVersion: state.Version, ChangeSeq: state.ChangeSeq}
	if err := outcome.Validate(); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	if err := writeCanonicalOperationOutcome(txn, request, scopeKey, operationKey, state, outcome); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	if err := s.runBeforeCanonicalMutationCommit(); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	return outcome, nil
}

func loadExpectedCanonicalOperationState(
	txn *badger.Txn,
	request memory.CanonicalOperationCommitRequest,
	scopeKey []byte,
) (memory.ScopeState, error) {
	state, err := loadCanonicalOperationScopeState(txn, request.Scope, scopeKey)
	if err != nil {
		return memory.ScopeState{}, err
	}

	if state.Version != request.ExpectedScopeVersion {
		return memory.ScopeState{}, memory.PermanentError(
			memory.CodeConflict, "canonical scope version changed", nil,
		)
	}

	return state, nil
}
