package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func eraseScrubPage(ctx context.Context, txn *Tx, journal *memory.ScopeEraseJournal) error {
	kind, next := eraseScrubPhase(journal.Stage)

	prefix, err := eraseRange(journal.Scope, kind)
	if err != nil {
		return err
	}

	cursor, done, err := eraseWalkPage(ctx, txn, prefix, journal.Cursor, func(row Row) error {
		return eraseScrubRecord(ctx, txn, *journal, row, kind)
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

func eraseScrubPhase(stage memory.ScopeEraseStage) (Family, memory.ScopeEraseStage) {
	switch stage {
	case memory.ScopeEraseScrubSources:
		return Source, memory.ScopeEraseScrubMessages
	case memory.ScopeEraseScrubMessages:
		return Message, memory.ScopeEraseScrubRevisions
	default:
		return Revision, memory.ScopeEraseScrubbed
	}
}

func eraseCheckDenied(ctx context.Context, txn *Tx, scope memory.Scope, record eraseRawRecord) error {
	kind := DeniedSource
	id := record.ID

	var denied any = &deniedSource{}

	if record.Message != nil {
		id = record.Message.SourceID
	}

	if record.Revision != nil {
		kind = DeniedRevision
		denied = &deniedRevision{}
	}

	key, err := eraseKey(scope, kind, id)
	if err != nil {
		return err
	}

	if err := getPrivacyRecord(ctx, txn, key, kind, denied); err != nil {
		return eraseStoreFailure("scope erase scrub target is not denied", err)
	}

	return nil
}

func eraseScrubRecord(ctx context.Context, txn *Tx, journal memory.ScopeEraseJournal, row Row, kind Family) error {
	record, err := erasePageRecord(ctx, txn, row, kind)
	if err != nil {
		return err
	}

	if record.Scope != journal.Scope {
		return eraseStoreFailure("scope erase scrub target scope differs", nil)
	}

	selected, err := eraseRecordSelected(ctx, txn, journal, record)
	if err != nil || !selected {
		return err
	}

	return eraseScrubOwnedRecord(ctx, txn, journal, record)
}

func eraseScrubOwnedRecord(ctx context.Context, txn *Tx, journal memory.ScopeEraseJournal,
	record eraseRawRecord) error {
	if err := eraseCheckDenied(ctx, txn, journal.Scope, record); err != nil {
		return err
	}

	lock, found, err := loadEraseBlobLock(ctx, txn, record.Payload.ID)
	if err != nil {
		return err
	}

	if !found || lock.Scope != journal.Scope || lock.OperationID != journal.OperationID {
		return eraseStoreFailure("scope erase scrub blob is not owned", nil)
	}

	if record.Revision != nil {
		if err := eraseRemoveHead(ctx, txn, journal.Scope, *record.Revision); err != nil {
			return err
		}
	}

	return erasePayloadTxn(ctx, txn, record.Payload)
}
