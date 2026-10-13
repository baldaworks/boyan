package badger

import (
	"errors"
	"slices"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func eraseRequestKind(request memory.ScopeEraseRequest) memory.ForgetKind {
	if request.Kind == "" {
		return memory.ForgetKindScope
	}

	return request.Kind
}

// Legacy adoption uses the persisted exact result, never freshly enrolled targets.
func adoptLegacyEraseOutcome(txn *badger.Txn, journal *memory.ScopeEraseJournal) error {
	key, err := badgerForgetOutcomeKey(journal.Scope, journal.ForgetKind(), journal.OperationID)
	if err != nil {
		return err
	}

	var stored badgerCanonicalForgetOutcome

	err = getCanonicalRecord(txn, key, badgerRecordForgetOutcome, &stored)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil
	}

	if err != nil {
		return err
	}

	if stored.Fingerprint != journal.Fingerprint {
		return memory.PermanentError(memory.CodeConflict, "legacy forget fingerprint differs", nil)
	}

	if err := validateStoredForgetOutcome(
		stored.Outcome,
		journal.Scope,
		journal.OperationID,
		journal.ForgetKind(),
	); err != nil {
		return err
	}

	if stored.Outcome.ReferencesTruncated {
		return eraseStoreFailure("legacy truncated forget has no target journal", nil)
	}

	journal.LegacyOutcome = true
	journal.Outcome = &stored.Outcome
	journal.SourceCount = uint64(len(stored.Outcome.Sources))
	journal.RevisionCount = uint64(len(stored.Outcome.Revisions))
	journal.Sources = stored.Outcome.Sources
	journal.Revisions = stored.Outcome.Revisions

	return nil
}

func eraseRecordSelected(txn *badger.Txn, journal memory.ScopeEraseJournal, record eraseRawRecord) (bool, error) {
	if journal.LegacyOutcome {
		return eraseLegacyRecordSelected(txn, journal, record)
	}

	if journal.ForgetKind() == memory.ForgetKindScope {
		return true, nil
	}

	if record.Source != nil {
		return record.ID == journal.SourceID, nil
	}

	if record.Message != nil {
		return record.Message.SourceID == journal.SourceID, nil
	}

	for _, evidence := range record.Revision.Evidence {
		if evidence.SourceID == journal.SourceID {
			return true, nil
		}
	}

	return false, nil
}

func eraseLegacyRecordSelected(txn *badger.Txn, journal memory.ScopeEraseJournal, record eraseRawRecord) (bool, error) {
	if record.Source != nil {
		return eraseLegacySourceSelected(txn, journal, *record.Source)
	}

	if record.Message != nil {
		return slices.Contains(journal.LegacySourceIDs, record.Message.SourceID), nil
	}

	ref := memory.RevisionRef{ItemID: record.Revision.ItemID, RevisionID: record.ID}

	return slices.Contains(journal.Outcome.Revisions, ref), nil
}

func eraseLegacySourceSelected(
	txn *badger.Txn,
	journal memory.ScopeEraseJournal,
	source memory.CanonicalSourceRecord,
) (bool, error) {
	for _, ref := range journal.Outcome.Sources {
		if memory.TurnSourceID(ref.ExportID) == source.SourceID {
			return true, nil
		}
	}

	ref, found, err := loadErasedSourceIdentity(txn, journal.Scope, source.SourceID)
	if err != nil {
		return false, err
	}

	if found {
		return slices.Contains(journal.Outcome.Sources, ref), nil
	}

	data, err := listedCanonicalSourcePayload(txn, source.Payload.ID)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	if !isBadgerPayloadValid(data, source.Payload) {
		return false, eraseStoreFailure("legacy forget source payload is invalid", nil)
	}

	ref, _, ok := canonicalSourceRef(data, journal.Scope, source.SourceID)
	if !ok {
		return false, eraseStoreFailure("legacy forget source identity is invalid", nil)
	}

	return slices.Contains(journal.Outcome.Sources, ref), nil
}
