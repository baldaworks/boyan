package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func (s *CanonicalStore) applyCanonicalMutationTxn(
	ctx context.Context,
	txn *badger.Txn,
	mutation memory.CanonicalMutation,
	scopeKey []byte,
	operationKey []byte,
) (memory.CanonicalMutationOutcome, error) {
	if err := checkScopeEraseFence(txn, mutation.Scope, ""); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	if err := prepareBoundaryIdentity(txn, mutation.Scope, mutation.BoundaryIdentity, operationKey); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	identity, err := prepareTurnIdentityMutation(txn, mutation)
	if err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	if outcome, found, err := loadCanonicalMutationOutcome(txn, mutation, operationKey); found || err != nil {
		if err != nil {
			return memory.CanonicalMutationOutcome{}, err
		}

		if err := completeTurnIdentityReplay(txn, mutation, identity); err != nil {
			return memory.CanonicalMutationOutcome{}, err
		}

		return outcome, nil
	}

	if identity.Forgotten {
		return memory.CanonicalMutationOutcome{}, turnIdentityConflict("forgotten canonical source cannot be derived again")
	}

	if err := persistTurnCertificate(txn, mutation); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	return s.applyUnfencedCanonicalMutationTxn(ctx, txn, mutation, scopeKey, operationKey)
}

func (s *CanonicalStore) applyUnfencedCanonicalMutationTxn(
	ctx context.Context,
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

	if err := s.putCanonicalMutationRecords(ctx, txn, mutation); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	if err := persistBoundaryCertificate(txn, mutation.Scope, mutation.BoundaryIdentity); err != nil {
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

	if err := s.runBeforeCanonicalMutationCommit(); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	return outcome, nil
}

func completeTurnIdentityReplay(
	txn *badger.Txn,
	mutation memory.CanonicalMutation,
	identity memory.CanonicalTurnIdentityResult,
) error {
	if mutation.TurnIdentity != nil && !identity.Found {
		return turnIdentityConflict("committed canonical turn identity is unavailable")
	}

	return persistTurnCertificate(txn, mutation)
}

func (s *CanonicalStore) runBeforeCanonicalMutationCommit() error {
	if s.beforeCanonicalMutationCommit != nil {
		return s.beforeCanonicalMutationCommit()
	}

	return nil
}
