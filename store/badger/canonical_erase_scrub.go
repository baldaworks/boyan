package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func eraseScrubPage(txn *badger.Txn, journal *memory.ScopeEraseJournal) error {
	kind, next := eraseScrubPhase(journal.Stage)

	prefix, err := canonicalPrefix(journal.Scope, kind)
	if err != nil {
		return err
	}

	cursor, done, err := eraseWalkPage(txn, prefix, journal.Cursor, func(key []byte) error {
		return eraseScrubRecord(txn, *journal, key, kind)
	})
	if err != nil {
		return err
	}

	journal.Cursor = cursor
	if done {
		eraseTransition(journal, next)
	}

	return nil
}

func eraseScrubPhase(stage memory.ScopeEraseStage) (string, memory.ScopeEraseStage) {
	switch stage {
	case memory.ScopeEraseScrubSources:
		return badgerRecordSource, memory.ScopeEraseScrubMessages
	case memory.ScopeEraseScrubMessages:
		return badgerRecordMessage, memory.ScopeEraseScrubRevisions
	default:
		return badgerRecordRevision, memory.ScopeEraseScrubbed
	}
}

func eraseCheckDenied(txn *badger.Txn, scope memory.Scope, record eraseRawRecord) error {
	kind := badgerRecordDeniedSource
	id := record.ID

	var denied any = &badgerDeniedSource{}

	if record.Message != nil {
		id = record.Message.SourceID
	}

	if record.Revision != nil {
		kind = badgerRecordDeniedRevision
		denied = &badgerDeniedRevision{}
	}

	key, err := canonicalKey(scope, kind, id)
	if err != nil {
		return err
	}

	if err := getCanonicalRecord(txn, key, kind, denied); err != nil {
		return eraseStoreFailure("scope erase scrub target is not denied", err)
	}

	return nil
}

func eraseScrubRecord(txn *badger.Txn, journal memory.ScopeEraseJournal, key []byte, kind string) error {
	record, err := eraseRecord(txn, key, kind)
	if err != nil {
		return err
	}

	if record.Scope != journal.Scope {
		return eraseStoreFailure("scope erase scrub target scope differs", nil)
	}

	selected, err := eraseRecordSelected(txn, journal, record)
	if err != nil || !selected {
		return err
	}

	return eraseScrubOwnedRecord(txn, journal, record)
}

func eraseScrubOwnedRecord(txn *badger.Txn, journal memory.ScopeEraseJournal, record eraseRawRecord) error {
	if err := eraseCheckDenied(txn, journal.Scope, record); err != nil {
		return err
	}

	lock, found, err := loadEraseBlobLock(txn, record.Payload.ID)
	if err != nil {
		return err
	}

	if !found || lock.Scope != journal.Scope || lock.OperationID != journal.OperationID {
		return eraseStoreFailure("scope erase scrub blob is not owned", nil)
	}

	if record.Revision != nil {
		if err := eraseRemoveHead(txn, journal.Scope, *record.Revision); err != nil {
			return err
		}
	}

	return erasePayloadTxn(txn, record.Payload)
}
