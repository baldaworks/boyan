package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func checkCanonicalWriterFence(ctx context.Context, tx *Tx, scope memory.Scope) error {
	if err := requireSourcePrivacyReady(ctx, tx); err != nil {
		return err
	}

	key, err := privacyKey(scope, EraseFence, "pending")
	if err != nil {
		return err
	}

	var fence struct {
		OperationID string `json:"operation_id"`
	}

	err = getPrivacyRecord(ctx, tx, key, EraseFence, &fence)
	if privacyMissing(err) {
		return nil
	}

	if err != nil || fence.OperationID == "" {
		return proofStoreFailure("invalid scope erase fence", err)
	}

	return rejectPendingMutationErase(ctx, tx, scope, fence.OperationID)
}

func rejectPendingMutationErase(ctx context.Context, tx *Tx, scope memory.Scope, operationID string) error {
	key, err := privacyKey(scope, EraseJournal, operationID)
	if err != nil {
		return err
	}

	var journal memory.ScopeEraseJournal

	err = getPrivacyRecord(ctx, tx, key, EraseJournal, &journal)
	if err != nil || journal.Validate() != nil || journal.Scope != scope ||
		journal.OperationID != operationID || journal.Stage == memory.ScopeEraseComplete ||
		journal.Stage == memory.ScopeEraseRejected {
		return proofStoreFailure("scope erase fence has no pending journal", err)
	}

	return eraseConflict()
}

// Source history is a source-local existence check, never an ordered history page.
func hasPrivacySourceHistory(ctx context.Context, tx *Tx, scope memory.Scope, id string) (bool, error) {
	if err := tx.check(ctx, false); err != nil {
		return false, err
	}

	def, _ := familyDefinition(SourceRevision)
	request := Range{Family: SourceRevision, Scope: &scope, Prefix: [][]byte{[]byte(id)}, Limit: 1}

	where, args, err := rangePredicate(request, def)
	if err != nil {
		return false, tx.fail(err)
	}

	if tx.connection.pg != nil {
		where, args = pgPartitionPredicate(request, def, where, args)
	}

	var found bool

	err = scanOne(ctx, tx.q, "SELECT EXISTS(SELECT 1 FROM "+tx.connection.table(def.table)+where+" LIMIT 1)", args, &found)
	if err != nil {
		return false, tx.fail(databaseError(ctx, "source history", err))
	}

	return found, nil
}
