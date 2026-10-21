package canonicalsql

import (
	"context"
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

var errEraseBlobShared = errors.New("scope erase blob is owned by another erase")

type eraseBlobLock struct {
	Scope       memory.Scope `json:"scope"`
	OperationID string       `json:"operation_id"`
}

func loadEraseBlobLock(ctx context.Context, txn *Tx, id string) (eraseBlobLock, bool, error) {
	var lock eraseBlobLock

	key, err := eraseBlobLockKey(id)
	if err != nil {
		return lock, false, err
	}

	err = getPrivacyRecord(ctx, txn, key, EraseBlobLock, &lock)
	if privacyMissing(err) {
		return lock, false, nil
	}

	if err != nil || lock.OperationID == "" || lock.Scope.Validate() != nil {
		return lock, false, eraseStoreFailure("invalid scope erase blob lock", err)
	}

	return lock, true, nil
}

func lockEraseBlob(ctx context.Context, txn *Tx, journal memory.ScopeEraseJournal, id string) error {
	existing, found, err := loadEraseBlobLock(ctx, txn, id)
	if err != nil {
		return err
	}

	if found && (existing.Scope != journal.Scope || existing.OperationID != journal.OperationID) {
		return errEraseBlobShared
	}

	key, err := eraseBlobLockKey(id)
	if err != nil {
		return err
	}

	lock := eraseBlobLock{Scope: journal.Scope, OperationID: journal.OperationID}
	if err := putPrivacyRecord(ctx, txn, key, EraseBlobLock, lock); err != nil {
		return err
	}

	ownedKey, err := eraseOwnedKey(journal, EraseLockRef, id)
	if err != nil {
		return err
	}

	return putPrivacyRecord(ctx, txn, ownedKey, EraseLockRef, id)
}

func eraseLockPage(ctx context.Context, txn *Tx, journal *memory.ScopeEraseJournal) error {
	kind, next := eraseLockPhase(journal.Stage)

	prefix, err := eraseRange(journal.Scope, kind)
	if err != nil {
		return err
	}

	shared := false

	cursor, done, err := eraseWalkPage(ctx, txn, prefix, journal.Cursor, func(row Row) error {
		foreign, err := eraseLockRecord(ctx, txn, journal, row, kind)
		shared = shared || foreign

		return err
	})
	if err != nil {
		return err
	}

	if shared || journal.RejectionCode != "" {
		eraseTransition(journal, memory.ScopeEraseRejectLocks)

		return nil
	}

	journal.Cursor = cursor
	if done {
		if kind == Source &&
			journal.LegacyOutcome &&
			len(journal.LegacySourceIDs) != len(journal.Outcome.Sources) {
			return eraseStoreFailure("legacy erase source identities are incomplete", nil)
		}

		eraseTransition(journal, next)
	}

	return nil
}

func eraseLockPhase(stage memory.ScopeEraseStage) (Family, memory.ScopeEraseStage) {
	switch stage {
	case memory.ScopeEraseLockSources:
		return Source, memory.ScopeEraseLockMessages
	case memory.ScopeEraseLockMessages:
		return Message, memory.ScopeEraseLockRevisions
	default:
		return Revision, memory.ScopeEraseCheckSources
	}
}

func eraseCheckOwnershipPage(ctx context.Context, txn *Tx, journal *memory.ScopeEraseJournal) error {
	kind, next := eraseCheckPhase(journal.Stage)

	prefix, err := eraseGlobalPrefix(kind)
	if err != nil {
		return err
	}

	shared := false

	cursor, done, err := eraseWalkPage(ctx, txn, prefix, journal.Cursor, func(row Row) error {
		foreign, err := eraseForeignBlobReference(ctx, txn, *journal, row, kind)
		if err != nil {
			return err
		}

		shared = shared || foreign

		return nil
	})
	if err != nil {
		return err
	}

	if shared {
		eraseTransition(journal, memory.ScopeEraseRejectLocks)

		return nil
	}

	journal.Cursor = cursor
	if done {
		if next == memory.ScopeEraseDenySources && journal.LegacyOutcome {
			next = memory.ScopeEraseScrubSources
		}

		eraseTransition(journal, next)
	}

	return nil
}

func eraseCheckPhase(stage memory.ScopeEraseStage) (Family, memory.ScopeEraseStage) {
	switch stage {
	case memory.ScopeEraseCheckSources:
		return Source, memory.ScopeEraseCheckMessages
	case memory.ScopeEraseCheckMessages:
		return Message, memory.ScopeEraseCheckRevisions
	default:
		return Revision, memory.ScopeEraseDenySources
	}
}

func eraseReleasePage(ctx context.Context, txn *Tx, journal *memory.ScopeEraseJournal) error {
	prefix, err := eraseOwnedPrefix(*journal, EraseLockRef)
	if err != nil {
		return err
	}

	cursor, done, err := eraseWalkPage(ctx, txn, prefix, journal.Cursor, func(row Row) error {
		return eraseReleaseBlob(ctx, txn, *journal, row.Key)
	})
	if err != nil {
		return err
	}

	journal.Cursor = cursor

	if !done {
		return nil
	}

	if journal.Stage == memory.ScopeEraseRejectLocks {
		journal.Stage = memory.ScopeEraseRejected
	}

	if err := completeScopeEraseTxn(ctx, txn, *journal); err != nil {
		return err
	}

	if journal.Stage != memory.ScopeEraseRejected {
		journal.Stage = memory.ScopeEraseComplete
	}

	journal.Cursor = nil

	return nil
}

func erasePayloadTxn(ctx context.Context, txn *Tx, ref memory.PayloadRef) error {
	key, err := payloadRecordKey(ref)
	if err != nil {
		return err
	}

	if err := txn.Delete(ctx, key); err != nil {
		return err
	}

	marker, err := eraseKey(memory.Scope{}, ErasedBlob, ref.ID)
	if err != nil {
		return err
	}

	return putPrivacyRecord(ctx, txn, marker, ErasedBlob, true)
}

func eraseForeignBlobReference(
	ctx context.Context, txn *Tx,
	journal memory.ScopeEraseJournal,
	row Row,
	kind Family,
) (bool, error) {
	record, err := erasePageRecord(ctx, txn, row, kind)
	if err != nil {
		return false, err
	}

	if record.Scope == journal.Scope {
		selected, err := eraseRecordSelected(ctx, txn, journal, record)
		if err != nil || selected {
			return false, err
		}
	}

	lock, found, err := loadEraseBlobLock(ctx, txn, record.Payload.ID)
	if err != nil {
		return false, err
	}

	return found && lock.Scope == journal.Scope && lock.OperationID == journal.OperationID, nil
}

func eraseReleaseBlob(ctx context.Context, txn *Tx, journal memory.ScopeEraseJournal, key Key) error {
	var id string
	if err := getPrivacyRecord(ctx, txn, key, EraseLockRef, &id); err != nil {
		return err
	}

	lock, found, err := loadEraseBlobLock(ctx, txn, id)
	if err != nil {
		return err
	}

	if !found || lock.Scope != journal.Scope || lock.OperationID != journal.OperationID {
		return eraseStoreFailure("scope erase blob lock ownership differs", nil)
	}

	blobKey, err := eraseBlobLockKey(id)
	if err != nil {
		return err
	}

	if err := txn.Delete(ctx, blobKey); err != nil {
		return err
	}

	return txn.Delete(ctx, key)
}

func eraseLockRecord(ctx context.Context, txn *Tx, journal *memory.ScopeEraseJournal, row Row,
	kind Family) (bool, error) {
	record, err := erasePageRecord(ctx, txn, row, kind)
	if err != nil {
		return false, err
	}

	if record.Scope != journal.Scope {
		return false, eraseStoreFailure("scope erase lock target scope differs", nil)
	}

	selected, selectErr := eraseRecordSelected(ctx, txn, *journal, record)
	if selectErr != nil || !selected {
		return false, selectErr
	}

	if eraseSourceTargetOverLimit(journal, record) {
		return false, nil
	}

	if record.Source != nil && journal.LegacyOutcome {
		journal.LegacySourceIDs = append(journal.LegacySourceIDs, record.ID)
	}

	err = lockEraseBlob(ctx, txn, *journal, record.Payload.ID)
	if errors.Is(err, errEraseBlobShared) {
		return true, nil
	}

	return false, err
}

func eraseSourceTargetOverLimit(journal *memory.ScopeEraseJournal, record eraseRawRecord) bool {
	if record.Revision == nil || journal.ForgetKind() != memory.ForgetKindSource {
		return false
	}

	if journal.PreflightRevisionCount < memory.MaxScopeEraseReferences {
		journal.PreflightRevisionCount++
	}

	if journal.PreflightRevisionCount >= memory.MaxScopeEraseReferences {
		journal.RejectionCode = memory.CodeLimitExceeded

		return true
	}

	return false
}
