package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func writeCanonicalForgetOutcome(
	txn *badger.Txn,
	request memory.CanonicalForgetCommitRequest,
	key []byte,
	scopeKey []byte,
	state memory.ScopeState,
	outcome memory.ForgetOutcome,
) error {
	changeKey, err := badgerScopeChangeKey(request.Scope, state.ChangeSeq)
	if err != nil {
		return err
	}

	change := memory.ScopeChange{
		RequiresRebuild: outcome.ReferencesTruncated,
		Sequence:        state.ChangeSeq,
		OperationID:     request.OperationID,
		OccurredAt:      request.ForgottenAt.UTC(),
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

	return putCanonicalRecord(txn, key, badgerRecordForgetOutcome, badgerCanonicalForgetOutcome{
		Fingerprint: request.Fingerprint,
		Outcome:     outcome,
	})
}
