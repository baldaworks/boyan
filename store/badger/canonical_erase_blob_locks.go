package badger

import (
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

const (
	badgerRecordEraseSourceIdentity = "erase-source-identity"
	badgerRecordErasedBlob          = "erased-blob"
	badgerRecordEraseBlobLock       = "erase-blob-lock"
	badgerRecordEraseLockRef        = "erase-lock-ref"
	badgerRecordEraseSourceRef      = "erase-source-ref"
)

var errEraseBlobShared = errors.New("scope erase blob is owned by another erase")

type eraseBlobLock struct {
	Scope       memory.Scope `json:"scope"`
	OperationID string       `json:"operation_id"`
}

func eraseBlobLockKey(id string) ([]byte, error) {
	return canonicalKey(payloadScope(), badgerRecordEraseBlobLock, id)
}
func eraseOwnedKey(journal memory.ScopeEraseJournal, kind, id string) ([]byte, error) {
	return memory.TupleKey(
		canonicalNamespace,
		canonicalVersion,
		kind,
		journal.Scope.Key,
		string(journal.Scope.Kind),
		journal.OperationID,
		id,
	)
}
func eraseOwnedPrefix(journal memory.ScopeEraseJournal, kind string) ([]byte, error) {
	return memory.TupleKey(
		canonicalNamespace,
		canonicalVersion,
		kind,
		journal.Scope.Key,
		string(journal.Scope.Kind),
		journal.OperationID,
	)
}

func loadEraseBlobLock(txn *badger.Txn, id string) (eraseBlobLock, bool, error) {
	var lock eraseBlobLock

	key, err := eraseBlobLockKey(id)
	if err != nil {
		return lock, false, err
	}

	err = getCanonicalRecord(txn, key, badgerRecordEraseBlobLock, &lock)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return lock, false, nil
	}

	if err != nil || lock.OperationID == "" || lock.Scope.Validate() != nil {
		return lock, false, eraseStoreFailure("invalid scope erase blob lock", err)
	}

	return lock, true, nil
}

func checkEraseBlobWrite(txn *badger.Txn, id string) error {
	if err := checkErasedBlob(txn, id); err != nil {
		return err
	}

	_, found, err := loadEraseBlobLock(txn, id)
	if err != nil {
		return err
	}

	if found {
		return eraseConflict()
	}

	return nil
}

func lockEraseBlob(txn *badger.Txn, journal memory.ScopeEraseJournal, id string) error {
	existing, found, err := loadEraseBlobLock(txn, id)
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
	if err := putCanonicalRecord(txn, key, badgerRecordEraseBlobLock, lock); err != nil {
		return err
	}

	ownedKey, err := eraseOwnedKey(journal, badgerRecordEraseLockRef, id)
	if err != nil {
		return err
	}

	return putCanonicalRecord(txn, ownedKey, badgerRecordEraseLockRef, id)
}

func eraseLockPage(txn *badger.Txn, journal *memory.ScopeEraseJournal) error {
	kind, next := eraseLockPhase(journal.Stage)

	prefix, err := canonicalPrefix(journal.Scope, kind)
	if err != nil {
		return err
	}

	shared := false

	cursor, done, err := eraseWalkPage(txn, prefix, journal.Cursor, func(key []byte) error {
		foreign, err := eraseLockRecord(txn, journal, key, kind)
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
		if kind == badgerRecordSource &&
			journal.LegacyOutcome &&
			len(journal.LegacySourceIDs) != len(journal.Outcome.Sources) {
			return eraseStoreFailure("legacy erase source identities are incomplete", nil)
		}

		eraseTransition(journal, next)
	}

	return nil
}

func eraseLockPhase(stage memory.ScopeEraseStage) (string, memory.ScopeEraseStage) {
	switch stage {
	case memory.ScopeEraseLockSources:
		return badgerRecordSource, memory.ScopeEraseLockMessages
	case memory.ScopeEraseLockMessages:
		return badgerRecordMessage, memory.ScopeEraseLockRevisions
	default:
		return badgerRecordRevision, memory.ScopeEraseCheckSources
	}
}

func eraseCheckOwnershipPage(txn *badger.Txn, journal *memory.ScopeEraseJournal) error {
	kind, next := eraseCheckPhase(journal.Stage)

	prefix, err := eraseGlobalPrefix(kind)
	if err != nil {
		return err
	}

	shared := false

	cursor, done, err := eraseWalkPage(txn, prefix, journal.Cursor, func(key []byte) error {
		foreign, err := eraseForeignBlobReference(txn, *journal, key, kind)
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

func eraseCheckPhase(stage memory.ScopeEraseStage) (string, memory.ScopeEraseStage) {
	switch stage {
	case memory.ScopeEraseCheckSources:
		return badgerRecordSource, memory.ScopeEraseCheckMessages
	case memory.ScopeEraseCheckMessages:
		return badgerRecordMessage, memory.ScopeEraseCheckRevisions
	default:
		return badgerRecordRevision, memory.ScopeEraseDenySources
	}
}

func eraseReleasePage(txn *badger.Txn, journal *memory.ScopeEraseJournal) error {
	prefix, err := eraseOwnedPrefix(*journal, badgerRecordEraseLockRef)
	if err != nil {
		return err
	}

	cursor, done, err := eraseWalkPage(txn, prefix, journal.Cursor, func(key []byte) error {
		return eraseReleaseBlob(txn, *journal, key)
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

	if err := completeScopeEraseTxn(txn, *journal); err != nil {
		return err
	}

	if journal.Stage != memory.ScopeEraseRejected {
		journal.Stage = memory.ScopeEraseComplete
	}

	journal.Cursor = nil

	return nil
}

func verifyEraseLocksReleased(txn *badger.Txn, journal memory.ScopeEraseJournal) error {
	prefix, err := eraseOwnedPrefix(journal, badgerRecordEraseLockRef)
	if err != nil {
		return err
	}

	options := badger.DefaultIteratorOptions
	options.PrefetchValues = false

	iterator := txn.NewIterator(options)
	defer iterator.Close()

	iterator.Seek(prefix)

	if iterator.ValidForPrefix(prefix) {
		return eraseConflict()
	}

	return nil
}

func checkErasedBlob(txn *badger.Txn, id string) error {
	key, err := canonicalKey(payloadScope(), badgerRecordErasedBlob, id)
	if err != nil {
		return err
	}

	var erased bool

	err = getCanonicalRecord(txn, key, badgerRecordErasedBlob, &erased)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil
	}

	if err != nil || !erased {
		return eraseStoreFailure("invalid erased payload marker", err)
	}

	return memory.PermanentError(memory.CodeConflict, "canonical payload was forgotten", nil)
}

func erasePayloadTxn(txn *badger.Txn, ref memory.PayloadRef) error {
	key, err := payloadRecordKey(ref)
	if err != nil {
		return err
	}

	if err := txn.Delete(key); err != nil {
		return err
	}

	marker, err := canonicalKey(payloadScope(), badgerRecordErasedBlob, ref.ID)
	if err != nil {
		return err
	}

	return putCanonicalRecord(txn, marker, badgerRecordErasedBlob, true)
}

func eraseForeignBlobReference(
	txn *badger.Txn,
	journal memory.ScopeEraseJournal,
	key []byte,
	kind string,
) (bool, error) {
	record, err := eraseRecord(txn, key, kind)
	if err != nil {
		return false, err
	}

	if record.Scope == journal.Scope {
		selected, err := eraseRecordSelected(txn, journal, record)
		if err != nil || selected {
			return false, err
		}
	}

	lock, found, err := loadEraseBlobLock(txn, record.Payload.ID)
	if err != nil {
		return false, err
	}

	return found && lock.Scope == journal.Scope && lock.OperationID == journal.OperationID, nil
}

func eraseReleaseBlob(txn *badger.Txn, journal memory.ScopeEraseJournal, key []byte) error {
	var id string
	if err := getCanonicalRecord(txn, key, badgerRecordEraseLockRef, &id); err != nil {
		return err
	}

	lock, found, err := loadEraseBlobLock(txn, id)
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

	if err := txn.Delete(blobKey); err != nil {
		return err
	}

	return txn.Delete(key)
}

func eraseLockRecord(txn *badger.Txn, journal *memory.ScopeEraseJournal, key []byte, kind string) (bool, error) {
	record, err := eraseRecord(txn, key, kind)
	if err != nil {
		return false, err
	}

	if record.Scope != journal.Scope {
		return false, eraseStoreFailure("scope erase lock target scope differs", nil)
	}

	selected, selectErr := eraseRecordSelected(txn, *journal, record)
	if selectErr != nil || !selected {
		return false, selectErr
	}

	if eraseSourceTargetOverLimit(journal, record) {
		return false, nil
	}

	if record.Source != nil && journal.LegacyOutcome {
		journal.LegacySourceIDs = append(journal.LegacySourceIDs, record.ID)
	}

	err = lockEraseBlob(txn, *journal, record.Payload.ID)
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
