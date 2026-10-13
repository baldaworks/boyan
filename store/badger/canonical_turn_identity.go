package badger

import (
	"context"
	"errors"
	"slices"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

const badgerRecordTurnCertificate = "turn-certificate"

var _ memory.CanonicalTurnIdentityStore = (*CanonicalStore)(nil)

// VerifyCanonicalTurn proves immutable input without changing canonical state.
// Forgotten certificate-backed sources certify replay only, never derivation.
func (s *CanonicalStore) VerifyCanonicalTurn(
	ctx context.Context,
	turn memory.Turn,
) (memory.CanonicalTurnIdentityResult, error) {
	if err := canonicalContextError(ctx); err != nil {
		return memory.CanonicalTurnIdentityResult{}, err
	}

	normalized, err := memory.NormalizeCanonicalTurn(turn)
	if err != nil {
		return memory.CanonicalTurnIdentityResult{}, err
	}

	if s == nil {
		return memory.CanonicalTurnIdentityResult{}, eraseStoreFailure("canonical badger store is closed", nil)
	}

	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()

	if s.db == nil {
		return memory.CanonicalTurnIdentityResult{}, eraseStoreFailure("canonical badger store is closed", nil)
	}

	var result memory.CanonicalTurnIdentityResult

	err = s.db.View(func(txn *badger.Txn) error {
		var err error

		result, err = verifyCanonicalTurnTxn(txn, normalized)
		if err != nil {
			return err
		}

		return canonicalContextError(ctx)
	})
	if err != nil {
		return memory.CanonicalTurnIdentityResult{}, canonicalStoreError("verify canonical turn identity", err)
	}

	return result, nil
}

func verifyCanonicalTurnTxn(txn *badger.Txn, turn memory.Turn) (memory.CanonicalTurnIdentityResult, error) {
	result, err := loadTurnIdentitySource(txn, turn)
	if err != nil {
		return result, err
	}

	certificate, certified, err := loadTurnCertificate(txn, turn.Scope, turn.SourceID)
	if err != nil {
		return result, err
	}

	if !result.Found {
		if certified {
			return result, eraseStoreFailure("canonical turn certificate source is missing", nil)
		}

		return result, nil
	}

	result.Forgotten, err = isDeniedCanonicalSource(txn, turn.Scope, turn.SourceID)
	if err != nil {
		return result, err
	}

	if certified {
		return verifyCertifiedTurn(txn, turn, result, certificate)
	}

	if result.Forgotten {
		return result, turnIdentityConflict("forgotten legacy turn identity cannot be verified")
	}

	result.Messages, err = verifyLegacyTurn(txn, turn, result.Source)

	return result, err
}

func loadTurnIdentitySource(txn *badger.Txn, turn memory.Turn) (memory.CanonicalTurnIdentityResult, error) {
	var result memory.CanonicalTurnIdentityResult

	key, err := canonicalKey(turn.Scope, badgerRecordSource, turn.SourceID)
	if err != nil {
		return result, err
	}

	err = getCanonicalRecord(txn, key, badgerRecordSource, &result.Source)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return result, rejectKnownLegacyTurnIdentity(txn, turn)
	}

	if err != nil {
		return result, eraseStoreFailure("decode canonical turn source", err)
	}

	if err := result.Source.Validate(); err != nil {
		return result, eraseStoreFailure("invalid canonical turn source", err)
	}

	if result.Source.Scope != turn.Scope || result.Source.SourceID != turn.SourceID {
		return result, eraseStoreFailure("canonical turn source identity differs", nil)
	}

	result.Found = true

	return result, nil
}

func loadTurnCertificate(
	txn *badger.Txn,
	scope memory.Scope,
	sourceID string,
) (memory.CanonicalTurnCertificate, bool, error) {
	var certificate memory.CanonicalTurnCertificate

	key, err := canonicalKey(scope, badgerRecordTurnCertificate, sourceID)
	if err != nil {
		return certificate, false, err
	}

	err = getCanonicalRecord(txn, key, badgerRecordTurnCertificate, &certificate)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return certificate, false, nil
	}

	if err != nil {
		return certificate, false, eraseStoreFailure("decode canonical turn certificate", err)
	}

	if err := certificate.Validate(); err != nil {
		return certificate, false, eraseStoreFailure("invalid canonical turn certificate", err)
	}

	if certificate.SourceID != sourceID {
		return certificate, false, eraseStoreFailure("canonical turn certificate source differs", nil)
	}

	return certificate, true, nil
}

func verifyCertifiedTurn(
	txn *badger.Txn,
	turn memory.Turn,
	result memory.CanonicalTurnIdentityResult,
	certificate memory.CanonicalTurnCertificate,
) (memory.CanonicalTurnIdentityResult, error) {
	fingerprint, err := memory.CanonicalTurnFingerprint(turn)
	if err != nil {
		return result, err
	}

	ids := turnMessageIDs(turn)
	if fingerprint != certificate.Fingerprint || !slices.Equal(ids, certificate.MessageIDs) {
		return result, turnIdentityConflict("canonical turn authoritative content differs")
	}

	result.Messages, err = loadCertifiedTurnMessages(txn, turn, result.Forgotten)
	if err != nil {
		return result, err
	}

	if !result.Forgotten {
		if _, err := loadTurnIdentityPayload(txn, result.Source.Payload); err != nil {
			return result, err
		}
	}

	return result, nil
}

func turnMessageIDs(turn memory.Turn) []string {
	ids := make([]string, 0, len(turn.Messages))
	for _, message := range turn.Messages {
		ids = append(ids, message.MessageID)
	}

	return ids
}

func turnIdentityConflict(message string) error {
	return memory.PermanentError(memory.CodeConflict, message, nil)
}

// Known compatibility keys and durable deny markers are not evidence of a new
// native source. Decline certification rather than enrolling forgotten data.
func rejectKnownLegacyTurnIdentity(txn *badger.Txn, turn memory.Turn) error {
	key, err := canonicalKey(turn.Scope, badgerRecordSource, turn.ExportID)
	if err != nil {
		return err
	}

	var legacy memory.CanonicalSourceRecord

	err = getCanonicalRecord(txn, key, badgerRecordSource, &legacy)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return rejectDeniedTurnIdentifiers(txn, turn)
	}

	if err != nil {
		return eraseStoreFailure("decode legacy export-keyed source", err)
	}

	if err := legacy.Validate(); err != nil {
		return eraseStoreFailure("invalid legacy export-keyed source", err)
	}

	if legacy.SourceID != turn.ExportID || legacy.Scope != turn.Scope {
		return eraseStoreFailure("legacy export-keyed source identity differs", nil)
	}

	return turnIdentityConflict("legacy export-keyed turn identity cannot be verified")
}

func rejectDeniedTurnIdentifiers(txn *badger.Txn, turn memory.Turn) error {
	for _, sourceID := range []string{turn.SourceID, turn.ExportID} {
		denied, err := isDeniedCanonicalSource(txn, turn.Scope, sourceID)
		if err != nil {
			return eraseStoreFailure("read canonical turn denial", err)
		}

		if denied {
			return turnIdentityConflict("retained canonical source identity cannot be verified")
		}
	}

	return nil
}
