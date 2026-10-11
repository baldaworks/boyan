package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func (s *CanonicalStore) applyCanonicalMutationTxn(
	txn *badger.Txn,
	mutation memory.CanonicalMutation,
	scopeKey []byte,
	operationKey []byte,
) (memory.CanonicalMutationOutcome, error) {
	if outcome, ok, err := loadCanonicalMutationOutcome(txn, mutation, operationKey); ok || err != nil {
		return outcome, err
	}

	state, err := loadCanonicalMutationScopeState(txn, mutation.Scope, scopeKey)
	if err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	if state.Version != mutation.ExpectedScopeVersion {
		return memory.CanonicalMutationOutcome{}, memory.PermanentError(
			memory.CodeConflict,
			"canonical scope version changed",
			nil,
		)
	}

	if err := s.putCanonicalMutationRecords(txn, mutation); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	state.Version++
	state.ChangeSeq++

	outcome := buildCanonicalMutationOutcome(state, mutation)
	if err := outcome.Validate(); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	if err := writeCanonicalMutationOutcome(txn, mutation, scopeKey, operationKey, state, outcome); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	if s.beforeCanonicalMutationCommit != nil {
		if err := s.beforeCanonicalMutationCommit(); err != nil {
			return memory.CanonicalMutationOutcome{}, err
		}
	}

	return outcome, nil
}
