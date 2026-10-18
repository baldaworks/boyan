package canonicalsql

import (
	"context"
	"slices"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func verifyCanonicalTurnTxn(ctx context.Context,
	txn *Tx,
	turn memory.Turn) (memory.CanonicalTurnIdentityResult,
	error) {
	result, err := loadTurnIdentitySource(ctx, txn, turn)
	if err != nil {
		return result, err
	}

	certificate, certified, err := loadTurnCertificate(ctx, txn, turn.Scope, turn.SourceID)
	if err != nil {
		return result, err
	}

	if !result.Found {
		if certified {
			return result, proofStoreFailure("canonical turn certificate source is missing", nil)
		}

		return result, nil
	}

	result.Forgotten, err = privacyDenied(ctx, txn, turn.Scope, DeniedSource, turn.SourceID)
	if err != nil {
		return result, err
	}

	if certified {
		return verifyCertifiedTurn(ctx, txn, turn, result, certificate)
	}

	if result.Forgotten {
		return result, turnIdentityConflict("forgotten legacy turn identity cannot be verified")
	}

	result.Messages, result.FullInputVerified, err = verifyLegacyTurn(ctx, txn, turn, result.Source)

	return result, err
}

func loadTurnIdentitySource(ctx context.Context,
	txn *Tx,
	turn memory.Turn) (memory.CanonicalTurnIdentityResult,
	error) {
	var result memory.CanonicalTurnIdentityResult

	key, err := privacyKey(turn.Scope, Source, turn.SourceID)
	if err != nil {
		return result, err
	}

	err = getPrivacyRecord(ctx, txn, key, Source, &result.Source)
	if privacyMissing(err) {
		return result, rejectKnownLegacyTurnIdentity(ctx, txn, turn)
	}

	if err != nil {
		return result, proofStoreFailure("decode canonical turn source", err)
	}

	if err := result.Source.Validate(); err != nil {
		return result, proofStoreFailure("invalid canonical turn source", err)
	}

	if result.Source.Scope != turn.Scope || result.Source.SourceID != turn.SourceID {
		return result, proofStoreFailure("canonical turn source identity differs", nil)
	}

	result.Found = true

	return result, nil
}

func verifyCertifiedTurn(
	ctx context.Context, txn *Tx,
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

	result.Messages, err = loadCertifiedTurnMessages(ctx, txn, turn, result.Forgotten)
	if err != nil {
		return result, err
	}

	if !result.Forgotten {
		if _, err := loadTurnIdentityPayload(ctx, txn, result.Source.Payload); err != nil {
			return result, err
		}
	}

	result.FullInputVerified = true

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
func rejectKnownLegacyTurnIdentity(ctx context.Context, txn *Tx, turn memory.Turn) error {
	key, err := privacyKey(turn.Scope, Source, turn.ExportID)
	if err != nil {
		return err
	}

	var legacy memory.CanonicalSourceRecord

	err = getPrivacyRecord(ctx, txn, key, Source, &legacy)
	if privacyMissing(err) {
		return rejectDeniedTurnIdentifiers(ctx, txn, turn)
	}

	if err != nil {
		return proofStoreFailure("decode legacy export-keyed source", err)
	}

	if err := legacy.Validate(); err != nil {
		return proofStoreFailure("invalid legacy export-keyed source", err)
	}

	if legacy.SourceID != turn.ExportID || legacy.Scope != turn.Scope {
		return proofStoreFailure("legacy export-keyed source identity differs", nil)
	}

	return turnIdentityConflict("legacy export-keyed turn identity cannot be verified")
}

func rejectDeniedTurnIdentifiers(ctx context.Context, txn *Tx, turn memory.Turn) error {
	for _, sourceID := range []string{turn.SourceID, turn.ExportID} {
		denied, err := privacyDenied(ctx, txn, turn.Scope, DeniedSource, sourceID)
		if err != nil {
			return proofStoreFailure("read canonical turn denial", err)
		}

		if denied {
			return turnIdentityConflict("retained canonical source identity cannot be verified")
		}
	}

	return nil
}
