package badger

import (
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func eraseConflict() error {
	return memory.RetryableError(memory.CodeConflict, "scope erase is pending or its baseline changed", nil)
}
func eraseStoreFailure(message string, cause error) error {
	return memory.PermanentError(memory.CodeStoreFailure, message, cause)
}

func loadScopeEraseFence(txn *badger.Txn, scope memory.Scope) (scopeEraseFence, bool, error) {
	var fence scopeEraseFence

	key, err := canonicalKey(scope, badgerRecordEraseFence, "pending")
	if err != nil {
		return fence, false, err
	}

	err = getCanonicalRecord(txn, key, badgerRecordEraseFence, &fence)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return fence, false, nil
	}

	if err != nil || fence.OperationID == "" {
		return fence, false, eraseStoreFailure("invalid scope erase fence", err)
	}

	return fence, true, nil
}

// owner is private transaction authority, never supplied through public write APIs.
func checkScopeEraseFence(txn *badger.Txn, scope memory.Scope, owner string) error {
	fence, found, err := loadScopeEraseFence(txn, scope)
	if err != nil || !found {
		return err
	}

	journal, found, err := loadScopeEraseTxn(txn, scope, fence.OperationID)
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

func validateScopeEraseOwnership(txn *badger.Txn, journal memory.ScopeEraseJournal) error {
	fence, found, err := loadScopeEraseFence(txn, journal.Scope)
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

	guard, err := loadScopeEraseGuard(txn)
	if err != nil {
		return err
	}

	if guard.Pending == 0 {
		return eraseStoreFailure("pending erase has no payload guard", nil)
	}

	return nil
}

func eraseGuardKey() ([]byte, error) {
	return canonicalKey(payloadScope(), badgerRecordEraseGuard, "pending")
}

func loadScopeEraseGuard(txn *badger.Txn) (scopeEraseGuard, error) {
	var guard scopeEraseGuard

	key, err := eraseGuardKey()
	if err != nil {
		return guard, err
	}

	err = getCanonicalRecord(txn, key, badgerRecordEraseGuard, &guard)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return guard, nil
	}

	if err != nil || guard.Pending == 0 {
		return guard, eraseStoreFailure("invalid scope erase payload guard", err)
	}

	return guard, nil
}

// Raw payload APIs cannot prove a scope. Conservatively stop them while any
// erase is pending. Normal scoped canonical writes in other scopes stay usable.
func checkScopeErasePayloadGuard(txn *badger.Txn) error {
	guard, err := loadScopeEraseGuard(txn)
	if err != nil {
		return err
	}

	if guard.Pending != 0 {
		return eraseConflict()
	}

	return nil
}

func installScopeErase(txn *badger.Txn, journal memory.ScopeEraseJournal) error {
	guard, err := loadScopeEraseGuard(txn)
	if err != nil {
		return err
	}

	if guard.Pending == ^uint64(0) {
		return eraseStoreFailure("scope erase guard overflow", nil)
	}

	guard.Pending++

	if err := putScopeEraseJournal(txn, journal); err != nil {
		return err
	}

	fenceKey, err := canonicalKey(journal.Scope, badgerRecordEraseFence, "pending")
	if err != nil {
		return err
	}

	fence := scopeEraseFence{OperationID: journal.OperationID}

	if err := putCanonicalRecord(txn, fenceKey, badgerRecordEraseFence, fence); err != nil {
		return err
	}

	guardKey, err := eraseGuardKey()
	if err != nil {
		return err
	}

	return putCanonicalRecord(txn, guardKey, badgerRecordEraseGuard, guard)
}

func putScopeEraseJournal(txn *badger.Txn, journal memory.ScopeEraseJournal) error {
	if err := journal.Validate(); err != nil {
		return eraseStoreFailure("invalid scope erase progress", err)
	}

	key, err := canonicalKey(journal.Scope, badgerRecordEraseJournal, journal.OperationID)
	if err != nil {
		return err
	}

	return putCanonicalRecord(txn, key, badgerRecordEraseJournal, journal)
}

func completeScopeEraseTxn(txn *badger.Txn, journal memory.ScopeEraseJournal) error {
	guard, err := loadScopeEraseGuard(txn)
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
	if err := putScopeEraseJournal(txn, journal); err != nil {
		return err
	}

	fenceKey, err := canonicalKey(journal.Scope, badgerRecordEraseFence, "pending")
	if err != nil {
		return err
	}

	if err := txn.Delete(fenceKey); err != nil {
		return err
	}

	key, err := eraseGuardKey()
	if err != nil {
		return err
	}

	guard.Pending--
	if guard.Pending == 0 {
		return txn.Delete(key)
	}

	return putCanonicalRecord(txn, key, badgerRecordEraseGuard, guard)
}

func putFencedDenyRecord(txn *badger.Txn, scope memory.Scope, key []byte, kind string, value any) error {
	if err := checkScopeEraseFence(txn, scope, ""); err != nil {
		return err
	}

	return putBadgerDenyRecord(txn, key, kind, value)
}

func eraseTerminal(stage memory.ScopeEraseStage) bool {
	return stage == memory.ScopeEraseComplete || stage == memory.ScopeEraseRejected
}
