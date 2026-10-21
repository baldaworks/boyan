package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func eraseDenyPage(ctx context.Context, txn *Tx, journal *memory.ScopeEraseJournal) error {
	kind := Source

	next := memory.ScopeEraseDenyRevisions
	if journal.Stage == memory.ScopeEraseDenyRevisions {
		kind = Revision
		next = memory.ScopeEraseCommitOutcome
	}

	prefix, err := eraseRange(journal.Scope, kind)
	if err != nil {
		return err
	}

	cursor, done, err := eraseWalkPage(ctx, txn, prefix, journal.Cursor, func(row Row) error {
		record, err := erasePageRecord(ctx, txn, row, kind)
		if err != nil {
			return err
		}

		if record.Scope != journal.Scope {
			return eraseStoreFailure("scope erase deny target differs", nil)
		}

		selected, err := eraseRecordSelected(ctx, txn, *journal, record)
		if err != nil || !selected {
			return err
		}

		return eraseDenyRecord(ctx, txn, journal, record)
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

func eraseDenyRecord(ctx context.Context, txn *Tx, journal *memory.ScopeEraseJournal, record eraseRawRecord) error {
	if record.Source != nil {
		return eraseDenySource(ctx, txn, journal, record)
	}

	return eraseDenyRevision(ctx, txn, journal, record)
}

func eraseDenySource(ctx context.Context, txn *Tx, journal *memory.ScopeEraseJournal, record eraseRawRecord) error {
	ref, err := eraseSourceRef(ctx, txn, *journal, *record.Source)
	if err != nil {
		return err
	}

	identityKey, err := eraseKey(journal.Scope, EraseSourceIdentity, record.ID)
	if err != nil {
		return err
	}

	if err := putMutationImmutableRecord(ctx, txn, identityKey, EraseSourceIdentity, ref); err != nil {
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

	key, err := eraseKey(journal.Scope, DeniedSource, record.ID)
	if err != nil {
		return err
	}

	return putDenyRecord(ctx, txn, key, DeniedSource, deniedSource{
		SourceID: record.ID,
		DeniedAt: journal.ForgottenAt,
	})
}

func eraseDenyRevision(ctx context.Context, txn *Tx, journal *memory.ScopeEraseJournal, record eraseRawRecord) error {
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

	key, err := eraseKey(journal.Scope, DeniedRevision, record.ID)
	if err != nil {
		return err
	}

	if err := putDenyRecord(ctx, txn, key, DeniedRevision, deniedRevision{
		RevisionID: record.ID,
		DeniedAt:   journal.ForgottenAt,
	}); err != nil {
		return err
	}

	return eraseRemoveHead(ctx, txn, journal.Scope, *record.Revision)
}

func eraseBoundReferences(journal *memory.ScopeEraseJournal) {
	if len(journal.Sources)+len(journal.Revisions) > memory.MaxScopeEraseReferences {
		journal.Sources = nil
		journal.Revisions = nil
		journal.ReferencesTruncated = true
	}
}

func eraseCommitOutcome(ctx context.Context, txn *Tx, journal *memory.ScopeEraseJournal) error {
	scopeKey, err := proofScopeKey(journal.Scope)
	if err != nil {
		return err
	}

	state, err := loadProofScopeState(ctx, txn, journal.Scope, scopeKey)
	if err != nil {
		return err
	}

	if state.Version != journal.BaselineVersion || state.ChangeSeq != journal.BaselineChangeSeq {
		return eraseConflict()
	}

	state, err = advanceForgetState(state)
	if err != nil {
		return err
	}

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

	key, err := forgetOutcomeKey(journal.Scope, journal.ForgetKind(), journal.OperationID)
	if err != nil {
		return err
	}

	if err := writeCanonicalForgetOutcome(ctx, txn, request, key, scopeKey, state, outcome); err != nil {
		return err
	}

	journal.Outcome = &outcome
	eraseTransition(journal, memory.ScopeEraseScrubSources)

	return nil
}
