package canonicalsql

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Scan one bounded slice of a prior outcome's already bounded references.

func eraseCopyOutcomeRefs(ctx context.Context, txn *Tx, journal *memory.ScopeEraseJournal, key Key) error {
	var stored canonicalForgetOutcome
	if err := getPrivacyRecord(ctx, txn, key, ForgetOutcome, &stored); err != nil {
		return err
	}

	if err := stored.Outcome.Validate(); err != nil {
		return eraseStoreFailure("invalid prior forget outcome", err)
	}

	if stored.Outcome.Scope != journal.Scope {
		return eraseStoreFailure("prior forget outcome scope differs", nil)
	}

	start := int(journal.ReferenceOffset)
	if start > len(stored.Outcome.Sources) {
		return eraseStoreFailure("scope erase reference offset differs", nil)
	}

	end := min(start+scopeEraseBatchSize, len(stored.Outcome.Sources))
	for _, ref := range stored.Outcome.Sources[start:end] {
		identityKey, err := eraseOwnedKey(*journal, EraseSourceRef, memory.TurnSourceID(ref.ExportID))
		if err != nil {
			return err
		}

		if err := putMutationImmutableRecord(ctx, txn, identityKey, EraseSourceRef, ref); err != nil {
			return err
		}
	}

	_, values, _ := key.values()
	journal.Cursor = stampKey(values)

	journal.ReferenceOffset = uint32(end)
	if end == len(stored.Outcome.Sources) {
		journal.ReferenceOffset = 0
	}

	return nil
}

func eraseSourceRef(
	ctx context.Context, txn *Tx,
	journal memory.ScopeEraseJournal,
	source memory.CanonicalSourceRecord,
) (memory.SourceRef, error) {
	data, err := listedCanonicalSourcePayload(ctx, txn, source.Payload.ID)
	if err == nil {
		if !validErasePayload(data, source.Payload) {
			return memory.SourceRef{}, eraseStoreFailure("scope erase source payload is invalid", nil)
		}

		ref, _, ok := canonicalSourceRef(data, journal.Scope, source.SourceID)
		if !ok {
			return memory.SourceRef{}, eraseStoreFailure("scope erase source identity is unavailable", nil)
		}

		return ref, nil
	}

	if !privacyMissing(err) {
		return memory.SourceRef{}, err
	}

	return eraseMissingSourceRef(ctx, txn, journal, source.SourceID)
}

func eraseMissingSourceRef(
	ctx context.Context, txn *Tx,
	journal memory.ScopeEraseJournal,
	sourceID string,
) (memory.SourceRef, error) {
	ref, found, err := loadErasedSourceIdentity(ctx, txn, journal.Scope, sourceID)
	if err != nil || found {
		return ref, err
	}

	key, err := eraseOwnedKey(journal, EraseSourceRef, sourceID)
	if err != nil {
		return memory.SourceRef{}, err
	}

	ref = memory.SourceRef{}
	if err := getPrivacyRecord(ctx, txn, key, EraseSourceRef, &ref); err != nil {
		return ref, eraseStoreFailure("scope erase source identity evidence is absent", err)
	}

	if err := ref.Validate(); err != nil {
		return ref, eraseStoreFailure("scope erase source identity evidence is invalid", err)
	}

	if ref.Scope != journal.Scope {
		return ref, eraseStoreFailure("scope erase source identity evidence scope differs", nil)
	}

	return ref, nil
}

func loadErasedSourceIdentity(ctx context.Context, txn *Tx, scope memory.Scope,
	sourceID string) (memory.SourceRef, bool, error) {
	var ref memory.SourceRef

	key, err := eraseKey(scope, EraseSourceIdentity, sourceID)
	if err != nil {
		return ref, false, err
	}

	err = getPrivacyRecord(ctx, txn, key, EraseSourceIdentity, &ref)
	if privacyMissing(err) {
		return ref, false, nil
	}

	if err != nil {
		return ref, false, eraseStoreFailure("decode erased source identity", err)
	}

	if ref.Scope != scope || ref.Validate() != nil {
		return ref, false, eraseStoreFailure("invalid erased source identity", nil)
	}

	return ref, true, nil
}

func eraseReadOutcomePage(ctx context.Context, tx *Tx, j *memory.ScopeEraseJournal) error {
	scan := Range{Family: ForgetOutcome, Scope: &j.Scope, Limit: 1}

	var key Key

	if len(j.Cursor) > 0 {
		var err error

		key, err = keyFromFrame(ForgetOutcome, j.Cursor)
		if err != nil {
			return err
		}

		if key.Scope != j.Scope {
			return corruptRow()
		}

		if j.ReferenceOffset != 0 {
			return eraseCopyOutcomeRefs(ctx, tx, j, key)
		}

		scan.After = &key
	}

	rows, err := tx.Scan(ctx, scan)
	if err != nil {
		return err
	}

	if len(rows) == 0 {
		eraseTransition(j, memory.ScopeEraseLockSources)

		return nil
	}

	return eraseCopyOutcomeRefs(ctx, tx, j, rows[0].Key)
}
