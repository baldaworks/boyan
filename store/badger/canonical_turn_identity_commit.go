package badger

import (
	"bytes"
	"slices"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func prepareTurnIdentityMutation(
	txn *badger.Txn,
	mutation memory.CanonicalMutation,
) (memory.CanonicalTurnIdentityResult, error) {
	if mutation.TurnIdentity == nil {
		return memory.CanonicalTurnIdentityResult{}, validateCertifiedMutationRecords(txn, mutation)
	}

	turn, err := memory.NormalizeCanonicalTurn(mutation.TurnIdentity.Turn)
	if err != nil {
		return memory.CanonicalTurnIdentityResult{}, err
	}

	result, err := verifyCanonicalTurnTxn(txn, turn)
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

func persistTurnCertificate(txn *badger.Txn, mutation memory.CanonicalMutation) error {
	if mutation.TurnIdentity == nil {
		return nil
	}

	certificate := mutation.TurnIdentity.Certificate

	key, err := canonicalKey(mutation.Scope, badgerRecordTurnCertificate, certificate.SourceID)
	if err != nil {
		return err
	}

	return putCanonicalImmutableRecord(txn, key, badgerRecordTurnCertificate, certificate)
}

func validateCertifiedMutationRecords(txn *badger.Txn, mutation memory.CanonicalMutation) error {
	for _, source := range mutation.Sources {
		if err := validateCertifiedSourceRecord(txn, mutation.Scope, source); err != nil {
			return err
		}
	}

	for _, message := range mutation.Messages {
		if err := validateCertifiedMessageRecord(txn, mutation.Scope, message); err != nil {
			return err
		}
	}

	return nil
}

func validateCertifiedSourceRecord(txn *badger.Txn, scope memory.Scope, source memory.CanonicalSourceRecord) error {
	_, found, err := loadTurnCertificate(txn, scope, source.SourceID)
	if err != nil || !found {
		return err
	}

	key, err := canonicalKey(scope, badgerRecordSource, source.SourceID)
	if err != nil {
		return err
	}

	var stored memory.CanonicalSourceRecord
	if err := getCanonicalRecord(txn, key, badgerRecordSource, &stored); err != nil {
		return eraseStoreFailure("read certified source record", err)
	}

	if err := stored.Validate(); err != nil {
		return eraseStoreFailure("invalid certified source record", err)
	}

	if source != stored {
		return turnIdentityConflict("canonical mutation changes certified source record")
	}

	return nil
}

func validateCertifiedMessageRecord(txn *badger.Txn, scope memory.Scope, message memory.MessageRecord) error {
	certificate, found, err := loadTurnCertificate(txn, scope, message.SourceID)
	if err != nil || !found {
		return err
	}

	if !slices.Contains(certificate.MessageIDs, message.MessageID) {
		return turnIdentityConflict("canonical turn message is outside certified membership")
	}

	key, err := canonicalKey(scope, badgerRecordMessage, message.MessageID)
	if err != nil {
		return err
	}

	var stored memory.MessageRecord
	if err := getCanonicalRecord(txn, key, badgerRecordMessage, &stored); err != nil {
		return eraseStoreFailure("read certified message record", err)
	}

	if err := stored.Validate(); err != nil {
		return eraseStoreFailure("invalid certified message record", err)
	}

	if message != stored {
		return turnIdentityConflict("canonical mutation changes certified message record")
	}

	return nil
}
