package canonicalsql

import (
	"context"
	"errors"
	"io"
	"reflect"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func sameRestoreEraseIdentity(a, b memory.ScopeEraseJournal) bool {
	return a.Version == b.Version && a.Scope == b.Scope && a.OperationID == b.OperationID &&
		a.Fingerprint == b.Fingerprint && a.Kind == b.Kind && a.SourceID == b.SourceID &&
		a.SourceRef == b.SourceRef && a.ForgottenAt.Equal(b.ForgottenAt) &&
		a.BaselineVersion == b.BaselineVersion && a.BaselineChangeSeq == b.BaselineChangeSeq
}

// Cleanup is a second pass: source sequence order can place a lock/fence
// deletion before its terminal journal. Every pass is still behind loading.
func (o *Owner) replayRestoreCleanup(ctx context.Context, spool io.Reader, pending int) error {
	for {
		finished := false

		err := o.restoreTransaction(ctx, true, func(ctx context.Context, tx *Tx) error {
			err := tx.restoreCleanupStream(ctx, spool, pending)
			if errors.Is(err, io.EOF) {
				finished = true

				return nil
			}

			return err
		})
		if err != nil || finished {
			return err
		}
	}
}

func (tx *Tx) restoreCleanupStream(ctx context.Context, spool io.Reader, pending int) error {
	for n := 0; pending == 0 || n < pending; n++ {
		data, err := readRestoreFrame(ctx, spool)
		if err != nil {
			return err
		}

		record, key, err := decodeRestoreRecord(data)
		if err != nil {
			return err
		}

		if record.Deleted {
			if err := tx.restoreEraseTombstone(ctx, key); err != nil {
				return err
			}
		}

		tx.scopeStamps = nil
	}

	return nil
}

// Permanent restrictions are never released here. Only explicitly deleted
// temporary state belonging to a validated terminal journal can be removed.
//
//nolint:cyclop // Exact ownership is resolved independently for the three temporary families.
func (tx *Tx) restoreEraseTombstone(ctx context.Context, key Key) error {
	if key.Family != EraseFence && key.Family != EraseLockRef && key.Family != EraseBlobLock {
		return nil
	}

	value, err := tx.Get(ctx, key)
	if privacyMissing(err) {
		return nil
	}

	if err != nil {
		return err
	}

	scope := key.Scope

	var operation string

	switch key.Family {
	case EraseFence:
		var fence scopeEraseFence
		if err := Decode(EraseFence, value, &fence); err != nil {
			return err
		}

		operation = fence.OperationID
	case EraseLockRef:
		operation = string(key.Parts[0])
	case EraseBlobLock:
		var lock eraseBlobLock
		if err := Decode(EraseBlobLock, value, &lock); err != nil {
			return err
		}

		scope, operation = lock.Scope, lock.OperationID
	}

	journal, found, err := loadScopeEraseTxn(ctx, tx, scope, operation)
	if err != nil {
		return err
	}

	if !found || !eraseTerminal(journal.Stage) {
		return nil
	}

	return tx.Delete(ctx, key)
}

func (tx *Tx) restorePendingEraseCount(ctx context.Context) (uint64, error) {
	var count uint64

	err := tx.walkRestoreFamily(ctx, EraseFence, func(row Row) error {
		var fence scopeEraseFence
		if string(row.Key.Parts[0]) != "pending" ||
			Decode(EraseFence, row.Envelope, &fence) != nil {
			return invalid("canonical restore erase fence is invalid")
		}

		j, found, err := loadScopeEraseTxn(ctx, tx, row.Key.Scope, fence.OperationID)
		if err != nil {
			return err
		}

		if !found || eraseTerminal(j.Stage) {
			return invalid("canonical restore erase fence binding is invalid")
		}

		if count == ^uint64(0) {
			return limitError()
		}

		count++

		return nil
	})

	return count, err
}

func (tx *Tx) restoreEraseGuard(ctx context.Context) error {
	count, err := tx.restorePendingEraseCount(ctx)
	if err != nil {
		return err
	}

	key := Key{Family: EraseGuard}
	if count == 0 {
		return tx.Delete(ctx, key)
	}

	return putPrivacyRecord(ctx, tx, key, EraseGuard, scopeEraseGuard{Pending: count})
}

func (tx *Tx) validateRestoreErase(ctx context.Context) error {
	count, err := tx.restorePendingEraseCount(ctx)
	if err != nil {
		return err
	}

	guard, err := loadScopeEraseGuard(ctx, tx)
	if err != nil || guard.Pending != count {
		return invalid("canonical restore erase guard binding is invalid")
	}

	if err := tx.walkRestoreFamily(ctx, EraseJournal, func(row Row) error {
		journal, found, err := loadScopeEraseTxn(ctx, tx, row.Key.Scope, string(row.Key.Parts[0]))
		if err != nil || !found {
			return invalid("canonical restore erase journal is invalid")
		}

		if err := validateScopeEraseOwnership(ctx, tx, journal); err != nil {
			return err
		}

		if err := tx.validateRestoreEraseOutcome(ctx, journal); err != nil {
			return err
		}

		if eraseTerminal(journal.Stage) {
			return verifyEraseLocksReleased(ctx, tx, journal)
		}

		return nil
	}); err != nil {
		return err
	}

	return tx.validateRestoreEraseLocks(ctx)
}

func (tx *Tx) validateRestoreEraseOutcome(
	ctx context.Context,
	journal memory.ScopeEraseJournal,
) error {
	if err := tx.validateRestoreEraseBaseline(ctx, journal); err != nil {
		return err
	}

	if journal.Outcome == nil {
		return nil
	}

	key, err := forgetOutcomeKey(journal.Scope, journal.ForgetKind(), journal.OperationID)
	if err != nil {
		return err
	}

	var stored canonicalForgetOutcome
	if err := getPrivacyRecord(ctx, tx, key, ForgetOutcome, &stored); err != nil {
		return err
	}

	if err := validateStoredForgetOutcome(
		stored.Outcome,
		journal.Scope,
		journal.OperationID,
		journal.ForgetKind(),
	); err != nil {
		return err
	}

	if stored.Fingerprint != journal.Fingerprint ||
		!reflect.DeepEqual(stored.Outcome, *journal.Outcome) {
		return invalid("canonical restore erase outcome binding differs")
	}

	return nil
}

func (tx *Tx) validateRestoreEraseBaseline(
	ctx context.Context,
	journal memory.ScopeEraseJournal,
) error {
	version, change, err := restoreEraseBaseline(journal)
	if err != nil {
		return err
	}

	key, err := proofScopeKey(journal.Scope)
	if err != nil {
		return err
	}

	state, err := loadProofScopeState(ctx, tx, journal.Scope, key)
	if err != nil {
		return err
	}

	if state.Version < version || state.ChangeSeq < change ||
		(!eraseTerminal(journal.Stage) && (state.Version != version || state.ChangeSeq != change)) {
		return invalid("canonical restore erase baseline binding differs")
	}

	return nil
}

func restoreEraseBaseline(journal memory.ScopeEraseJournal) (uint64, uint64, error) {
	version, change := journal.BaselineVersion, journal.BaselineChangeSeq
	if journal.LegacyOutcome {
		// Adoption captures the current baseline after an older receipt already
		// committed. It must not advance that baseline a second time.
		if journal.Outcome == nil || journal.Outcome.ScopeVersion > version {
			return 0, 0, invalid("canonical restore legacy erase baseline differs")
		}

		return version, change, nil
	}

	if !restoreEraseAfterCommit(journal.Stage) {
		if journal.Outcome != nil {
			return 0, 0, invalid("canonical restore precommit erase has outcome")
		}

		return version, change, nil
	}

	if version == ^uint64(0) || change == ^uint64(0) || journal.Outcome == nil ||
		journal.Outcome.ScopeVersion != version+1 {
		return 0, 0, invalid("canonical restore committed erase baseline differs")
	}

	return version + 1, change + 1, nil
}

func restoreEraseAfterCommit(stage memory.ScopeEraseStage) bool {
	switch stage {
	case memory.ScopeEraseScrubSources,
		memory.ScopeEraseScrubMessages,
		memory.ScopeEraseScrubRevisions,
		memory.ScopeEraseScrubbed,
		memory.ScopeEraseReleaseLocks,
		memory.ScopeEraseComplete:
		return true
	default:
		return false
	}
}

func (tx *Tx) validateRestoreEraseLocks(ctx context.Context) error {
	if err := tx.walkRestoreFamily(ctx, EraseLockRef, func(row Row) error {
		var id string
		if Decode(EraseLockRef, row.Envelope, &id) != nil || id != string(row.Key.Parts[1]) {
			return invalid("canonical restore erase lock reference is invalid")
		}

		lock, found, err := loadEraseBlobLock(ctx, tx, id)
		if err != nil {
			return err
		}

		if !found || lock.Scope != row.Key.Scope || lock.OperationID != string(row.Key.Parts[0]) {
			return invalid("canonical restore erase lock ownership differs")
		}

		return nil
	}); err != nil {
		return err
	}

	return tx.walkRestoreFamily(ctx, EraseBlobLock, func(row Row) error {
		return tx.validateRestoreBlobLock(ctx, row)
	})
}

func (tx *Tx) validateRestoreBlobLock(ctx context.Context, row Row) error {
	lock, found, err := loadEraseBlobLock(ctx, tx, string(row.Key.Parts[0]))
	if err != nil || !found {
		return invalid("canonical restore erase blob lock is invalid")
	}

	journal, found, err := loadScopeEraseTxn(ctx, tx, lock.Scope, lock.OperationID)
	if err != nil || !found || eraseTerminal(journal.Stage) {
		return invalid("canonical restore erase blob lock journal differs")
	}

	key, err := eraseOwnedKey(journal, EraseLockRef, string(row.Key.Parts[0]))
	if err != nil {
		return err
	}

	var id string
	if err := getPrivacyRecord(ctx, tx, key, EraseLockRef, &id); err != nil {
		return err
	}

	if id != string(row.Key.Parts[0]) {
		return invalid("canonical restore erase blob reverse ownership differs")
	}

	return nil
}

func (tx *Tx) walkRestoreFamily(ctx context.Context, family Family, visit func(Row) error) error {
	scan := Range{Family: family, Limit: archivePageSize}
	for {
		rows, err := tx.Scan(ctx, scan)
		if err != nil {
			return err
		}

		if len(rows) == 0 {
			return nil
		}

		for _, row := range rows {
			if err := visit(row); err != nil {
				return err
			}
		}

		scan.After = &rows[len(rows)-1].Key
	}
}
