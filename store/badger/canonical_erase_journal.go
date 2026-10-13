package badger

import (
	"context"
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

const (
	badgerRecordEraseJournal = "erase-journal"
	badgerRecordEraseFence   = "erase-fence"
	badgerRecordEraseGuard   = "erase-guard"
)

type scopeEraseFence struct {
	OperationID string `json:"operation_id"`
}
type scopeEraseGuard struct {
	Pending uint64 `json:"pending"`
}

var _ memory.ScopeEraseStore = (*CanonicalStore)(nil)

// BeginScopeErase atomically freezes an exact scope at its current canonical baseline.
func (s *CanonicalStore) BeginScopeErase(
	ctx context.Context,
	request memory.ScopeEraseRequest,
) (memory.ScopeEraseJournal, error) {
	if err := request.Validate(); err != nil {
		return memory.ScopeEraseJournal{}, err
	}

	var result memory.ScopeEraseJournal

	err := s.eraseUpdate(ctx, func(txn *badger.Txn) error {
		journal, found, err := loadScopeEraseTxn(txn, request.Scope, request.OperationID)
		if err != nil {
			return err
		}

		if found {
			if err := validateEraseReplayRequest(journal, request); err != nil {
				return err
			}

			result = journal

			return validateScopeEraseOwnership(txn, journal)
		}

		if err := checkScopeEraseFence(txn, request.Scope, ""); err != nil {
			return err
		}

		scopeKey, err := badgerScopeKey(request.Scope)
		if err != nil {
			return err
		}

		state, err := loadCanonicalMutationScopeState(txn, request.Scope, scopeKey)
		if err != nil {
			return err
		}

		if state.Version != request.ExpectedScopeVersion {
			return eraseConflict()
		}

		result = buildScopeEraseJournal(request, state)

		if err := adoptLegacyEraseOutcome(txn, &result); err != nil {
			return err
		}

		return installScopeErase(txn, result)
	})

	return result, err
}

// LoadScopeErase reads validated progress; corrupt records never release the fence.
func (s *CanonicalStore) LoadScopeErase(
	ctx context.Context,
	scope memory.Scope,
	operationID string,
) (memory.ScopeEraseJournal, bool, error) {
	var (
		journal memory.ScopeEraseJournal
		found   bool
	)

	err := s.eraseView(ctx, func(txn *badger.Txn) error {
		var err error

		journal, found, err = loadScopeEraseTxn(txn, scope, operationID)
		if err != nil || !found {
			return err
		}

		return validateScopeEraseOwnership(txn, journal)
	})

	return journal, found, err
}

// CompleteScopeErase cannot bypass unfinished scrubbing. Only backend batch
// transactions can establish scrubbed progress; no public progress setter exists.
func (s *CanonicalStore) CompleteScopeErase(ctx context.Context, scope memory.Scope, operationID string) error {
	return s.eraseUpdate(ctx, func(txn *badger.Txn) error {
		journal, found, err := loadScopeEraseTxn(txn, scope, operationID)
		if err != nil {
			return err
		}

		if !found {
			return eraseStoreFailure("scope erase journal is missing", nil)
		}

		if err := validateScopeEraseOwnership(txn, journal); err != nil {
			return err
		}

		if journal.Stage == memory.ScopeEraseComplete {
			return nil
		}

		if journal.Stage != memory.ScopeEraseScrubbed {
			return eraseConflict()
		}

		if err := verifyEraseLocksReleased(txn, journal); err != nil {
			return err
		}

		return completeScopeEraseTxn(txn, journal)
	})
}

func loadScopeEraseTxn(
	txn *badger.Txn,
	scope memory.Scope,
	operationID string,
) (memory.ScopeEraseJournal, bool, error) {
	var journal memory.ScopeEraseJournal

	key, err := canonicalKey(scope, badgerRecordEraseJournal, operationID)
	if err != nil {
		return journal, false, err
	}

	err = getCanonicalRecord(txn, key, badgerRecordEraseJournal, &journal)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return journal, false, nil
	}

	if err != nil {
		return journal, false, eraseStoreFailure("decode scope erase journal", err)
	}

	if err := journal.Validate(); err != nil {
		return journal, false, eraseStoreFailure("invalid scope erase journal", err)
	}

	if journal.Scope != scope || journal.OperationID != operationID {
		return journal, false, eraseStoreFailure("scope erase journal identity differs", nil)
	}

	return journal, true, nil
}

func validateEraseReplayRequest(journal memory.ScopeEraseJournal, request memory.ScopeEraseRequest) error {
	if journal.Fingerprint != request.Fingerprint ||
		journal.ForgetKind() != eraseRequestKind(request) ||
		journal.SourceID != request.SourceID {
		return memory.PermanentError(memory.CodeConflict, "scope erase fingerprint or target differs", nil)
	}

	return nil
}

func buildScopeEraseJournal(request memory.ScopeEraseRequest, state memory.ScopeState) memory.ScopeEraseJournal {
	return memory.ScopeEraseJournal{
		Kind: request.Kind, SourceID: request.SourceID, SourceRef: request.SourceRef,
		Version:           1,
		Scope:             request.Scope,
		OperationID:       request.OperationID,
		Fingerprint:       request.Fingerprint,
		ForgottenAt:       request.ForgottenAt.UTC(),
		BaselineVersion:   state.Version,
		BaselineChangeSeq: state.ChangeSeq,
		Stage:             memory.ScopeErasePrepared,
	}
}
