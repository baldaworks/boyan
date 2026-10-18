package canonicalsql

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

const scopeEraseBatchSize = 128

// RunScopeErase resumes bounded durable work. Errors never mean completion.
func (s *Owner) RunScopeErase(
	ctx context.Context,
	request memory.ScopeEraseRequest,
) (memory.ScopeEraseJournal, error) {
	journal, err := s.BeginScopeErase(ctx, request)
	if err != nil {
		return memory.ScopeEraseJournal{}, err
	}

	for !eraseTerminal(journal.Stage) {
		journal, err = s.AdvanceScopeErase(ctx, request.Scope, request.OperationID)
		if err != nil {
			return journal, err
		}
	}

	if journal.Stage == memory.ScopeEraseRejected {
		return journal, eraseRejectedError(journal)
	}

	return journal, nil
}

// AdvanceScopeErase commits at most one fixed structural page. The backend,
// rather than caller-supplied progress, determines transitions and lock ownership.
func (s *Owner) AdvanceScopeErase(
	ctx context.Context,
	scope memory.Scope,
	operationID string,
) (memory.ScopeEraseJournal, error) {
	var result memory.ScopeEraseJournal

	err := s.Update(ctx, func(ctx context.Context, txn *Tx) error {
		journal, found, err := loadScopeEraseTxn(ctx, txn, scope, operationID)
		if err != nil {
			return err
		}

		if !found {
			return eraseStoreFailure("scope erase journal is missing", nil)
		}

		if err := validateScopeEraseOwnership(ctx, txn, journal); err != nil {
			return err
		}

		if eraseTerminal(journal.Stage) {
			result = journal

			return nil
		}

		if err := advanceScopeEraseTxn(ctx, txn, &journal); err != nil {
			return err
		}

		if err := putScopeEraseJournal(ctx, txn, journal); err != nil {
			return err
		}

		result = journal

		return nil
	})
	if err != nil {
		// Failed transactions must not expose provisional terminal progress.
		return memory.ScopeEraseJournal{}, err
	}

	if result.Stage == memory.ScopeEraseRejected {
		return result, eraseRejectedError(result)
	}

	return result, nil
}

func advanceScopeEraseTxn(ctx context.Context, txn *Tx, journal *memory.ScopeEraseJournal) error {
	switch journal.Stage {
	case memory.ScopeErasePrepared:
		eraseTransition(journal, memory.ScopeEraseReadOutcomes)

		return nil
	case memory.ScopeEraseReadOutcomes:
		return eraseReadOutcomePage(ctx, txn, journal)
	case memory.ScopeEraseLockSources, memory.ScopeEraseLockMessages, memory.ScopeEraseLockRevisions:
		return eraseLockPage(ctx, txn, journal)
	case memory.ScopeEraseCheckSources, memory.ScopeEraseCheckMessages, memory.ScopeEraseCheckRevisions:
		return eraseCheckOwnershipPage(ctx, txn, journal)
	case memory.ScopeEraseDenySources, memory.ScopeEraseDenyRevisions:
		return eraseDenyPage(ctx, txn, journal)
	case memory.ScopeEraseCommitOutcome:
		return eraseCommitOutcome(ctx, txn, journal)
	case memory.ScopeEraseScrubSources, memory.ScopeEraseScrubMessages, memory.ScopeEraseScrubRevisions:
		return eraseScrubPage(ctx, txn, journal)
	default:
		return advanceScopeEraseCleanup(ctx, txn, journal)
	}
}

func advanceScopeEraseCleanup(ctx context.Context, txn *Tx, journal *memory.ScopeEraseJournal) error {
	switch journal.Stage {
	case memory.ScopeEraseScrubbed:
		eraseTransition(journal, memory.ScopeEraseReleaseLocks)

		return nil
	case memory.ScopeEraseReleaseLocks, memory.ScopeEraseRejectLocks:
		return eraseReleasePage(ctx, txn, journal)
	default:
		return eraseStoreFailure("unsupported scope erase progress", nil)
	}
}

func eraseTransition(journal *memory.ScopeEraseJournal, stage memory.ScopeEraseStage) {
	journal.Stage = stage
	journal.Cursor = nil
	journal.ReferenceOffset = 0
}

func eraseSharedConflict() error {
	return memory.PermanentError(
		memory.CodeConflict,
		"scope erase cannot safely remove a payload shared with another scope",
		nil,
	)
}

func eraseRejectedError(journal memory.ScopeEraseJournal) error {
	if journal.RejectionCode == memory.CodeLimitExceeded {
		return memory.PermanentError(memory.CodeLimitExceeded, "source forget outcome exceeds the reference limit", nil)
	}

	return eraseSharedConflict()
}
