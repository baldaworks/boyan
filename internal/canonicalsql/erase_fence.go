package canonicalsql

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func eraseStoreFailure(message string, cause error) error {
	return memory.PermanentError(memory.CodeStoreFailure, message, cause)
}

func loadScopeEraseFence(ctx context.Context, txn *Tx, scope memory.Scope) (scopeEraseFence, bool, error) {
	var fence scopeEraseFence

	key, err := eraseKey(scope, EraseFence, "pending")
	if err != nil {
		return fence, false, err
	}

	err = getPrivacyRecord(ctx, txn, key, EraseFence, &fence)
	if privacyMissing(err) {
		return fence, false, nil
	}

	if err != nil || fence.OperationID == "" {
		return fence, false, eraseStoreFailure("invalid scope erase fence", err)
	}

	return fence, true, nil
}

// owner is private transaction authority, never supplied through public write APIs.
func checkScopeEraseFence(ctx context.Context, txn *Tx, scope memory.Scope, owner string) error {
	fence, found, err := loadScopeEraseFence(ctx, txn, scope)
	if err != nil || !found {
		return err
	}

	journal, found, err := loadScopeEraseTxn(ctx, txn, scope, fence.OperationID)
	if err != nil {
		return err
	}

	if !found || eraseTerminal(journal.Stage) {
		return eraseStoreFailure("scope erase fence has no pending journal", nil)
	}

	if owner == "" || owner != fence.OperationID {
		return eraseConflict()
	}

	return nil
}

func validateScopeEraseOwnership(ctx context.Context, txn *Tx, journal memory.ScopeEraseJournal) error {
	fence, found, err := loadScopeEraseFence(ctx, txn, journal.Scope)
	if err != nil {
		return err
	}

	if eraseTerminal(journal.Stage) {
		if found && fence.OperationID == journal.OperationID {
			return eraseStoreFailure("completed erase retains fence", nil)
		}

		return nil
	}

	if !found || fence.OperationID != journal.OperationID {
		return eraseStoreFailure("pending erase has no owned fence", nil)
	}

	guard, err := loadScopeEraseGuard(ctx, txn)
	if err != nil {
		return err
	}

	if guard.Pending == 0 {
		return eraseStoreFailure("pending erase has no payload guard", nil)
	}

	return nil
}

func loadScopeEraseGuard(ctx context.Context, txn *Tx) (scopeEraseGuard, error) {
	var guard scopeEraseGuard

	key, err := eraseGuardKey()
	if err != nil {
		return guard, err
	}

	err = getPrivacyRecord(ctx, txn, key, EraseGuard, &guard)
	if privacyMissing(err) {
		return guard, nil
	}

	if err != nil || guard.Pending == 0 {
		return guard, eraseStoreFailure("invalid scope erase payload guard", err)
	}

	return guard, nil
}

// Raw payload APIs cannot prove a scope. Conservatively stop them while any
// erase is pending. Normal scoped canonical writes in other scopes stay usable.

func installScopeErase(ctx context.Context, txn *Tx, journal memory.ScopeEraseJournal) error {
	guard, err := loadScopeEraseGuard(ctx, txn)
	if err != nil {
		return err
	}

	if guard.Pending == ^uint64(0) {
		return eraseStoreFailure("scope erase guard overflow", nil)
	}

	guard.Pending++

	if err := putScopeEraseJournal(ctx, txn, journal); err != nil {
		return err
	}

	fenceKey, err := eraseKey(journal.Scope, EraseFence, "pending")
	if err != nil {
		return err
	}

	fence := scopeEraseFence{OperationID: journal.OperationID}

	if err := putPrivacyRecord(ctx, txn, fenceKey, EraseFence, fence); err != nil {
		return err
	}

	guardKey, err := eraseGuardKey()
	if err != nil {
		return err
	}

	return putPrivacyRecord(ctx, txn, guardKey, EraseGuard, guard)
}

func putScopeEraseJournal(ctx context.Context, txn *Tx, journal memory.ScopeEraseJournal) error {
	if err := journal.Validate(); err != nil {
		return eraseStoreFailure("invalid scope erase progress", err)
	}

	key, err := eraseKey(journal.Scope, EraseJournal, journal.OperationID)
	if err != nil {
		return err
	}

	return putPrivacyRecord(ctx, txn, key, EraseJournal, journal)
}

func completeScopeEraseTxn(ctx context.Context, txn *Tx, journal memory.ScopeEraseJournal) error {
	guard, err := loadScopeEraseGuard(ctx, txn)
	if err != nil {
		return err
	}

	if guard.Pending == 0 {
		return eraseStoreFailure("scope erase guard is missing", nil)
	}

	if journal.Stage != memory.ScopeEraseRejected {
		journal.Stage = memory.ScopeEraseComplete
	}

	journal.Cursor = nil
	if err := putScopeEraseJournal(ctx, txn, journal); err != nil {
		return err
	}

	fenceKey, err := eraseKey(journal.Scope, EraseFence, "pending")
	if err != nil {
		return err
	}

	if err := txn.Delete(ctx, fenceKey); err != nil {
		return err
	}

	key, err := eraseGuardKey()
	if err != nil {
		return err
	}

	guard.Pending--
	if guard.Pending == 0 {
		return txn.Delete(ctx, key)
	}

	return putPrivacyRecord(ctx, txn, key, EraseGuard, guard)
}

func eraseTerminal(stage memory.ScopeEraseStage) bool {
	return stage == memory.ScopeEraseComplete || stage == memory.ScopeEraseRejected
}
