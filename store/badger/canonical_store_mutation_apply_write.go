package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func buildCanonicalMutationOutcome(
	state memory.ScopeState,
	mutation memory.CanonicalMutation,
) memory.CanonicalMutationOutcome {
	outcome := memory.CanonicalMutationOutcome{ScopeVersion: state.Version, ChangeSeq: state.ChangeSeq}
	for _, revision := range mutation.Revisions {
		outcome.RevisionIDs = append(outcome.RevisionIDs, revision.RevisionID)
	}

	return outcome
}

func writeCanonicalMutationOutcome(
	txn *badger.Txn,
	mutation memory.CanonicalMutation,
	scopeKey []byte,
	operationKey []byte,
	state memory.ScopeState,
	outcome memory.CanonicalMutationOutcome,
) error {
	change := memory.ScopeChange{
		Sequence:    state.ChangeSeq,
		OperationID: mutation.Operation.OperationID,
		OccurredAt:  mutation.Operation.CommittedAt,
		RevisionIDs: outcome.RevisionIDs,
	}

	changeKey, err := badgerScopeChangeKey(mutation.Scope, change.Sequence)
	if err != nil {
		return err
	}

	if err := putCanonicalRecord(txn, changeKey, badgerRecordChange, change); err != nil {
		return err
	}

	if err := putCanonicalRecord(txn, scopeKey, badgerRecordScope, state); err != nil {
		return err
	}

	return putCanonicalRecord(txn, operationKey, badgerRecordOperation, badgerCanonicalOperation{
		Fingerprint: mutation.Operation.Fingerprint,
		Outcome:     outcome,
	})
}
