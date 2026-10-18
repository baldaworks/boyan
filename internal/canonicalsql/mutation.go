package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// ApplyCanonicalMutation commits only the affected immutable records and cursors.
func (o *Owner) ApplyCanonicalMutation(ctx context.Context,
	mutation memory.CanonicalMutation) (memory.CanonicalMutationOutcome, error) {
	if err := mutationContext(ctx); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	mutation.BoundaryIdentity = cloneBoundaryIdentity(mutation.BoundaryIdentity)
	if err := mutation.Validate(); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	var outcome memory.CanonicalMutationOutcome

	err := o.Update(ctx, func(ctx context.Context, tx *Tx) error {
		var err error

		outcome, err = applyCanonicalMutationTxn(ctx, tx, mutation)

		return err
	})
	if err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	return outcome, nil
}

func mutationContext(ctx context.Context) error {
	if ctx == nil {
		return invalid("nil canonical context")
	}

	return ctx.Err()
}

func applyCanonicalMutationTxn(ctx context.Context, tx *Tx,
	mutation memory.CanonicalMutation) (memory.CanonicalMutationOutcome, error) {
	identity, err := prepareMutationIdentity(ctx, tx, mutation)
	if err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	outcome, found, err := loadMutationOutcome(ctx, tx, mutation.Scope,
		mutation.Operation.OperationID, mutation.Operation.Fingerprint)
	if err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	if found {
		return outcome, completeTurnIdentityReplay(ctx, tx, mutation, identity)
	}

	if identity.Forgotten {
		return memory.CanonicalMutationOutcome{}, turnIdentityConflict("forgotten canonical source cannot be derived again")
	}

	if err := persistTurnCertificate(ctx, tx, mutation); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	return applyNewCanonicalMutation(ctx, tx, mutation)
}

func prepareMutationIdentity(ctx context.Context, tx *Tx,
	mutation memory.CanonicalMutation) (memory.CanonicalTurnIdentityResult, error) {
	if err := checkCanonicalWriterFence(ctx, tx, mutation.Scope); err != nil {
		return memory.CanonicalTurnIdentityResult{}, err
	}

	key, err := privacyKey(mutation.Scope, Operation, mutation.Operation.OperationID)
	if err != nil {
		return memory.CanonicalTurnIdentityResult{}, err
	}

	if err := prepareBoundaryIdentity(ctx, tx, mutation.Scope, mutation.BoundaryIdentity,
		mutation.Operation.OperationID, key); err != nil {
		return memory.CanonicalTurnIdentityResult{}, err
	}

	return prepareTurnIdentityMutation(ctx, tx, mutation)
}

func completeTurnIdentityReplay(ctx context.Context, tx *Tx,
	mutation memory.CanonicalMutation, identity memory.CanonicalTurnIdentityResult) error {
	if mutation.TurnIdentity != nil && !identity.Found {
		return turnIdentityConflict("committed canonical turn identity is unavailable")
	}

	return persistTurnCertificate(ctx, tx, mutation)
}

func applyNewCanonicalMutation(ctx context.Context, tx *Tx,
	mutation memory.CanonicalMutation) (memory.CanonicalMutationOutcome, error) {
	state, expected, err := expectedMutationState(ctx, tx, mutation.Scope, mutation.ExpectedScopeVersion)
	if err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	if err := putCanonicalMutationRecords(ctx, tx, mutation); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	if err := persistBoundaryCertificate(ctx, tx, mutation.Scope, mutation.BoundaryIdentity); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	state.Version++
	state.ChangeSeq++

	outcome := memory.CanonicalMutationOutcome{ScopeVersion: state.Version, ChangeSeq: state.ChangeSeq}
	for _, revision := range mutation.Revisions {
		outcome.RevisionIDs = append(outcome.RevisionIDs, revision.RevisionID)
	}

	if err := outcome.Validate(); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	if err := writeMutationOutcome(ctx, tx, state, expected, mutation.Operation, outcome); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	return outcome, nil
}

func expectedMutationState(ctx context.Context, tx *Tx, scope memory.Scope,
	version uint64) (memory.ScopeState, *uint64, error) {
	key, err := proofScopeKey(scope)
	if err != nil {
		return memory.ScopeState{}, nil, err
	}

	state, err := loadProofScopeState(ctx, tx, scope, key)
	if err != nil {
		return memory.ScopeState{}, nil, err
	}

	if state.Version != version {
		return memory.ScopeState{}, nil, conflict()
	}

	if state.Version == ^uint64(0) || state.ChangeSeq == ^uint64(0) {
		return memory.ScopeState{}, nil, limitError()
	}

	_, err = tx.Get(ctx, key)
	if privacyMissing(err) {
		return state, nil, nil
	}

	return state, &version, err
}

func writeMutationOutcome(ctx context.Context, tx *Tx, state memory.ScopeState, expected *uint64,
	operation memory.OperationRecord, outcome memory.CanonicalMutationOutcome) error {
	change := memory.ScopeChange{Sequence: state.ChangeSeq, ScopeVersion: state.Version,
		OperationID: operation.OperationID, OccurredAt: operation.CommittedAt, RevisionIDs: outcome.RevisionIDs}

	key, err := proofChangeKey(state.Scope, state.ChangeSeq)
	if err != nil {
		return err
	}

	if err := putPrivacyRecord(ctx, tx, key, ScopeChange, change); err != nil {
		return err
	}

	if err := tx.CompareAndSwapScope(ctx, expected, state); err != nil {
		return err
	}

	key, err = privacyKey(state.Scope, Operation, operation.OperationID)
	if err != nil {
		return err
	}

	return putPrivacyRecord(ctx, tx, key, Operation, canonicalOperation{Fingerprint: operation.Fingerprint,
		Outcome: outcome})
}

type canonicalOperation struct {
	Fingerprint string                          `json:"fingerprint"`
	Outcome     memory.CanonicalMutationOutcome `json:"outcome"`
}

func loadMutationOutcome(ctx context.Context, tx *Tx, scope memory.Scope, id,
	fingerprint string) (memory.CanonicalMutationOutcome, bool, error) {
	key, err := privacyKey(scope, Operation, id)
	if err != nil {
		return memory.CanonicalMutationOutcome{}, false, err
	}

	var record canonicalOperation

	err = getPrivacyRecord(ctx, tx, key, Operation, &record)
	if privacyMissing(err) {
		return memory.CanonicalMutationOutcome{}, false, nil
	}

	found := err == nil
	if err != nil || !found {
		return memory.CanonicalMutationOutcome{}, found, err
	}

	if record.Fingerprint != fingerprint {
		return memory.CanonicalMutationOutcome{}, true, memory.PermanentError(memory.CodeConflict,
			"canonical operation identity was reused", nil)
	}

	if err := record.Outcome.Validate(); err != nil {
		return memory.CanonicalMutationOutcome{}, true, invalidStoredCanonicalOutcome(err)
	}

	if err := validateCanonicalOutcomeBinding(ctx, tx, scope, id, record.Outcome); err != nil {
		return memory.CanonicalMutationOutcome{}, true, err
	}

	return record.Outcome, true, nil
}

// CommitCanonicalOperation advances the same state/change contract for empty stages.
func (o *Owner) CommitCanonicalOperation(ctx context.Context,
	request memory.CanonicalOperationCommitRequest) (memory.CanonicalMutationOutcome, error) {
	if err := mutationContext(ctx); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	request.BoundaryIdentity = cloneBoundaryIdentity(request.BoundaryIdentity)
	if err := request.Validate(); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	var outcome memory.CanonicalMutationOutcome

	err := o.Update(ctx, func(ctx context.Context, tx *Tx) error {
		var err error

		outcome, err = commitCanonicalOperationTxn(ctx, tx, request)

		return err
	})
	if err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	return outcome, nil
}

func commitCanonicalOperationTxn(ctx context.Context, tx *Tx,
	request memory.CanonicalOperationCommitRequest) (memory.CanonicalMutationOutcome, error) {
	if err := checkCanonicalWriterFence(ctx, tx, request.Scope); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	key, err := privacyKey(request.Scope, Operation, request.OperationID)
	if err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	if err := prepareBoundaryIdentity(ctx, tx, request.Scope, request.BoundaryIdentity, request.OperationID,
		key); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	if outcome, found, err := loadMutationOutcome(ctx, tx, request.Scope, request.OperationID,
		request.Fingerprint); found || err != nil {
		return outcome, err
	}

	state, expected, err := expectedMutationState(ctx, tx, request.Scope, request.ExpectedScopeVersion)
	if err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	if err := persistBoundaryCertificate(ctx, tx, request.Scope, request.BoundaryIdentity); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	state.Version++
	state.ChangeSeq++
	outcome := memory.CanonicalMutationOutcome{ScopeVersion: state.Version, ChangeSeq: state.ChangeSeq}

	operation := memory.OperationRecord{OperationID: request.OperationID, Fingerprint: request.Fingerprint,
		CommittedAt: request.CommittedAt}
	if err := writeMutationOutcome(ctx, tx, state, expected, operation, outcome); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}

	return outcome, nil
}
