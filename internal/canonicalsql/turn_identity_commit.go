package canonicalsql

import (
	"bytes"
	"context"
	"slices"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func prepareTurnIdentityMutation(
	ctx context.Context, txn *Tx,
	mutation memory.CanonicalMutation,
) (memory.CanonicalTurnIdentityResult, error) {
	if mutation.TurnIdentity == nil {
		return memory.CanonicalTurnIdentityResult{}, validateCertifiedMutationRecords(ctx, txn, mutation)
	}

	turn, err := memory.NormalizeCanonicalTurn(mutation.TurnIdentity.Turn)
	if err != nil {
		return memory.CanonicalTurnIdentityResult{}, err
	}

	result, err := verifyCanonicalTurnTxn(ctx, txn, turn)
	if err != nil {
		return result, err
	}

	if err := validateTurnMutationBindings(mutation, turn, result); err != nil {
		return result, err
	}

	return result, nil
}

func validateTurnMutationBindings(
	mutation memory.CanonicalMutation,
	turn memory.Turn,
	result memory.CanonicalTurnIdentityResult,
) error {
	if len(mutation.Sources) != 1 ||
		mutation.Sources[0].SourceID != turn.SourceID ||
		len(mutation.Messages) != len(turn.Messages) {
		return turnIdentityConflict("canonical turn mutation membership differs")
	}

	if result.Found {
		if mutation.Sources[0] != result.Source || !slices.Equal(mutation.Messages, result.Messages) {
			return turnIdentityConflict("canonical turn mutation changes immutable records")
		}

		return nil
	}

	data, err := turnMutationPayload(mutation, mutation.Sources[0].Payload)
	if err != nil {
		return err
	}

	if err := verifyNativeTurnSourceData(turn, data); err != nil {
		return err
	}

	return validateNewTurnMutationMessages(mutation, turn)
}

func validateNewTurnMutationMessages(mutation memory.CanonicalMutation, turn memory.Turn) error {
	for index, message := range turn.Messages {
		record := mutation.Messages[index]
		if record.MessageID != message.MessageID || record.SourceID != turn.SourceID || record.Role != message.Role {
			return turnIdentityConflict("new canonical turn message identity differs")
		}

		data, err := turnMutationPayload(mutation, record.Payload)
		if err != nil {
			return err
		}

		if !bytes.Equal(data, []byte(message.Text)) {
			return turnIdentityConflict("new canonical turn message content differs")
		}
	}

	return nil
}

func turnMutationPayload(mutation memory.CanonicalMutation, ref memory.PayloadRef) ([]byte, error) {
	for _, payload := range mutation.Payloads {
		if payload.Ref == ref {
			return payload.Data, nil
		}
	}

	return nil, turnIdentityConflict("canonical turn mutation lacks authoritative payload")
}

func persistTurnCertificate(ctx context.Context, txn *Tx, mutation memory.CanonicalMutation) error {
	if mutation.TurnIdentity == nil {
		return nil
	}

	certificate := mutation.TurnIdentity.Certificate

	key, err := privacyKey(mutation.Scope, TurnCertificate, certificate.SourceID)
	if err != nil {
		return err
	}

	return putImmutablePrivacyRecord(ctx, txn, key, certificate)
}

func validateCertifiedMutationRecords(ctx context.Context, txn *Tx, mutation memory.CanonicalMutation) error {
	for _, source := range mutation.Sources {
		if err := validateCertifiedSourceRecord(ctx, txn, mutation.Scope, source); err != nil {
			return err
		}
	}

	for _, message := range mutation.Messages {
		if err := validateCertifiedMessageRecord(ctx, txn, mutation.Scope, message); err != nil {
			return err
		}
	}

	return nil
}

func validateCertifiedSourceRecord(ctx context.Context,
	txn *Tx,
	scope memory.Scope,
	source memory.CanonicalSourceRecord) error {
	_, found, err := loadTurnCertificate(ctx, txn, scope, source.SourceID)
	if err != nil || !found {
		return err
	}

	key, err := privacyKey(scope, Source, source.SourceID)
	if err != nil {
		return err
	}

	var stored memory.CanonicalSourceRecord
	if err := getPrivacyRecord(ctx, txn, key, Source, &stored); err != nil {
		return proofStoreFailure("read certified source record", err)
	}

	if err := stored.Validate(); err != nil {
		return proofStoreFailure("invalid certified source record", err)
	}

	if source != stored {
		return turnIdentityConflict("canonical mutation changes certified source record")
	}

	return nil
}

func validateCertifiedMessageRecord(ctx context.Context,
	txn *Tx,
	scope memory.Scope,
	message memory.MessageRecord) error {
	certificate, found, err := loadTurnCertificate(ctx, txn, scope, message.SourceID)
	if err != nil || !found {
		return err
	}

	if !slices.Contains(certificate.MessageIDs, message.MessageID) {
		return turnIdentityConflict("canonical turn message is outside certified membership")
	}

	key, err := privacyKey(scope, Message, message.MessageID)
	if err != nil {
		return err
	}

	var stored memory.MessageRecord
	if err := getPrivacyRecord(ctx, txn, key, Message, &stored); err != nil {
		return proofStoreFailure("read certified message record", err)
	}

	if err := stored.Validate(); err != nil {
		return proofStoreFailure("invalid certified message record", err)
	}

	if message != stored {
		return turnIdentityConflict("canonical mutation changes certified message record")
	}

	return nil
}
