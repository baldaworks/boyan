package canonicalsql

import (
	"context"
	"slices"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func validateCanonicalOutcomeBinding(
	ctx context.Context, txn *Tx,
	scope memory.Scope,
	operationID string,
	outcome memory.CanonicalMutationOutcome,
) error {
	change, err := loadCanonicalOutcomeChange(ctx, txn, scope, outcome.ChangeSeq)
	if err != nil {
		return err
	}

	if !canonicalOutcomeChangeMatches(change, operationID, outcome) {
		return invalidStoredCanonicalOutcome(nil)
	}

	return validateCanonicalOutcomeScopeState(ctx, txn, scope, outcome)
}

func loadCanonicalOutcomeChange(
	ctx context.Context, txn *Tx,
	scope memory.Scope,
	changeSeq uint64,
) (memory.ScopeChange, error) {
	changeKey, err := proofChangeKey(scope, changeSeq)
	if err != nil {
		return memory.ScopeChange{}, invalidStoredCanonicalOutcome(err)
	}

	var change memory.ScopeChange
	if err := getPrivacyRecord(ctx, txn, changeKey, ScopeChange, &change); err != nil {
		return memory.ScopeChange{}, invalidStoredCanonicalOutcome(err)
	}

	if err := change.Validate(); err != nil {
		return memory.ScopeChange{}, invalidStoredCanonicalOutcome(err)
	}

	return change, nil
}

func canonicalOutcomeChangeMatches(
	change memory.ScopeChange,
	operationID string,
	outcome memory.CanonicalMutationOutcome,
) bool {
	scopeVersion := change.ScopeVersion
	if scopeVersion == 0 {
		// Legacy change records predate an explicit scope-version binding. The
		// canonical writers advanced both counters together, so their values match.
		scopeVersion = change.Sequence
	}

	return change.Sequence == outcome.ChangeSeq && scopeVersion == outcome.ScopeVersion &&
		change.OperationID == operationID && slices.Equal(change.RevisionIDs, outcome.RevisionIDs)
}

func validateCanonicalOutcomeScopeState(
	ctx context.Context, txn *Tx,
	scope memory.Scope,
	outcome memory.CanonicalMutationOutcome,
) error {
	scopeKey, err := proofScopeKey(scope)
	if err != nil {
		return invalidStoredCanonicalOutcome(err)
	}

	state, err := loadProofScopeState(ctx, txn, scope, scopeKey)
	if err != nil {
		return invalidStoredCanonicalOutcome(err)
	}

	if state.Version < outcome.ScopeVersion || state.ChangeSeq < outcome.ChangeSeq {
		return invalidStoredCanonicalOutcome(nil)
	}

	return nil
}

func invalidStoredCanonicalOutcome(err error) error {
	return memory.PermanentError(
		memory.CodeStoreFailure,
		"stored canonical operation outcome is invalid",
		err,
	)
}
