package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func eraseDenyPage(txn *badger.Txn, journal *memory.ScopeEraseJournal) error {
	kind := badgerRecordSource

	next := memory.ScopeEraseDenyRevisions
	if journal.Stage == memory.ScopeEraseDenyRevisions {
		kind = badgerRecordRevision
		next = memory.ScopeEraseCommitOutcome
	}

	prefix, err := canonicalPrefix(journal.Scope, kind)
	if err != nil {
		return err
	}

	cursor, done, err := eraseWalkPage(txn, prefix, journal.Cursor, func(key []byte) error {
		record, err := eraseRecord(txn, key, kind)
		if err != nil {
			return err
		}

		if record.Scope != journal.Scope {
			return eraseStoreFailure("scope erase deny target differs", nil)
		}

		selected, err := eraseRecordSelected(txn, *journal, record)
		if err != nil || !selected {
			return err
		}

		return eraseDenyRecord(txn, journal, record)
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

func eraseDenyRecord(txn *badger.Txn, journal *memory.ScopeEraseJournal, record eraseRawRecord) error {
	if record.Source != nil {
		return eraseDenySource(txn, journal, record)
	}

	return eraseDenyRevision(txn, journal, record)
}

func eraseDenySource(txn *badger.Txn, journal *memory.ScopeEraseJournal, record eraseRawRecord) error {
	ref, err := eraseSourceRef(txn, *journal, *record.Source)
	if err != nil {
		return err
	}

	identityKey, err := canonicalKey(journal.Scope, badgerRecordEraseSourceIdentity, record.ID)
	if err != nil {
		return err
	}

	if err := putCanonicalImmutableRecord(txn, identityKey, badgerRecordEraseSourceIdentity, ref); err != nil {
		return err
	}

	if journal.SourceCount == ^uint64(0) {
		return eraseStoreFailure("scope erase count overflow", nil)
	}

	journal.SourceCount++
	if !journal.ReferencesTruncated {
		journal.Sources = append(journal.Sources, ref)
	}

	eraseBoundReferences(journal)

	key, err := canonicalKey(journal.Scope, badgerRecordDeniedSource, record.ID)
	if err != nil {
		return err
	}

	return putBadgerDenyRecord(txn, key, badgerRecordDeniedSource, badgerDeniedSource{
		SourceID: record.ID,
		DeniedAt: journal.ForgottenAt,
	})
}

func eraseDenyRevision(txn *badger.Txn, journal *memory.ScopeEraseJournal, record eraseRawRecord) error {
	if journal.RevisionCount == ^uint64(0) {
		return eraseStoreFailure("scope erase count overflow", nil)
	}

	journal.RevisionCount++
	if !journal.ReferencesTruncated {
		journal.Revisions = append(journal.Revisions, memory.RevisionRef{
			ItemID:     record.Revision.ItemID,
			RevisionID: record.ID,
		})
	}

	eraseBoundReferences(journal)

	key, err := canonicalKey(journal.Scope, badgerRecordDeniedRevision, record.ID)
	if err != nil {
		return err
	}

	if err := putBadgerDenyRecord(txn, key, badgerRecordDeniedRevision, badgerDeniedRevision{
		RevisionID: record.ID,
		DeniedAt:   journal.ForgottenAt,
	}); err != nil {
		return err
	}

	return eraseRemoveHead(txn, journal.Scope, *record.Revision)
}

func eraseBoundReferences(journal *memory.ScopeEraseJournal) {
	if len(journal.Sources)+len(journal.Revisions) > memory.MaxScopeEraseReferences {
		journal.Sources = nil
		journal.Revisions = nil
		journal.ReferencesTruncated = true
	}
}

func eraseCommitOutcome(txn *badger.Txn, journal *memory.ScopeEraseJournal) error {
	scopeKey, err := badgerScopeKey(journal.Scope)
	if err != nil {
		return err
	}

	state, err := loadCanonicalMutationScopeState(txn, journal.Scope, scopeKey)
	if err != nil {
		return err
	}

	if state.Version != journal.BaselineVersion || state.ChangeSeq != journal.BaselineChangeSeq {
		return eraseConflict()
	}

	state.Version++
	state.ChangeSeq++

	outcome := memory.ForgetOutcome{
		SchemaVersion: memory.DerivedSchemaVersionV1, OperationID: journal.OperationID, Kind: journal.ForgetKind(),
		Scope: journal.Scope, ForgottenAt: journal.ForgottenAt.UTC(), ScopeVersion: state.Version,
		Sources: journal.Sources, Revisions: journal.Revisions,
		SourceCount:         journal.SourceCount,
		RevisionCount:       journal.RevisionCount,
		ReferencesTruncated: journal.ReferencesTruncated,
	}
	if err := outcome.Validate(); err != nil {
		return err
	}

	request := memory.CanonicalForgetCommitRequest{
		Scope:       journal.Scope,
		OperationID: journal.OperationID,
		Kind:        journal.ForgetKind(),
		Fingerprint: journal.Fingerprint,
		ForgottenAt: journal.ForgottenAt,
		Sources:     journal.Sources,
		Revisions:   journal.Revisions,
	}

	key, err := badgerForgetOutcomeKey(journal.Scope, journal.ForgetKind(), journal.OperationID)
	if err != nil {
		return err
	}

	if err := writeCanonicalForgetOutcome(txn, request, key, scopeKey, state, outcome); err != nil {
		return err
	}

	journal.Outcome = &outcome
	eraseTransition(journal, memory.ScopeEraseScrubSources)

	return nil
}
