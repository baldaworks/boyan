package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func writeCanonicalOperationOutcome(
	txn *badger.Txn,
	request memory.CanonicalOperationCommitRequest,
	scopeKey []byte,
	operationKey []byte,
	state memory.ScopeState,
	outcome memory.CanonicalMutationOutcome,
) error {
	changeKey, err := badgerScopeChangeKey(request.Scope, state.ChangeSeq)
	if err != nil {
		return err
	}
	change := memory.ScopeChange{
		Sequence:    state.ChangeSeq,
		OperationID: request.OperationID,
		OccurredAt:  request.CommittedAt,
	}
	if err := putCanonicalRecord(txn, changeKey, badgerRecordChange, change); err != nil {
		return err
	}
	if err := putCanonicalRecord(txn, scopeKey, badgerRecordScope, state); err != nil {
		return err
	}
	return putCanonicalRecord(txn, operationKey, badgerRecordOperation, badgerCanonicalOperation{
		Fingerprint: request.Fingerprint,
		Outcome:     outcome,
	})
}
