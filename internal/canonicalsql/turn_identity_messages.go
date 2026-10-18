package canonicalsql

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Legacy sources lack an ordered membership manifest. Read only their exact
// source partition and one overflow sentinel to prove bounded membership.
const maxTurnProofMessages = 32

func scanLegacyTurnMessages(ctx context.Context, txn *Tx, turn memory.Turn) ([]memory.MessageRecord, error) {
	rows, err := txn.MessagesBySource(ctx, turn.Scope, []byte(turn.SourceID), nil, maxTurnProofMessages+1)
	if err != nil {
		return nil, err
	}

	if len(rows) > maxTurnProofMessages {
		return nil, turnIdentityConflict("legacy turn message membership is not bounded")
	}

	records := make(map[string]memory.MessageRecord, len(rows))
	for _, row := range rows {
		var record memory.MessageRecord
		if err := Decode(Message, row.Envelope, &record); err != nil {
			return nil, err
		}

		if err := record.Validate(); err != nil {
			return nil, proofStoreFailure("invalid legacy source message", err)
		}

		records[record.MessageID] = record
	}

	if len(records) != len(turn.Messages) {
		return nil, turnIdentityConflict("legacy turn message membership differs")
	}

	return compareLegacyTurnMessages(ctx, txn, turn, records)
}

func compareLegacyTurnMessages(
	ctx context.Context, txn *Tx,
	turn memory.Turn,
	records map[string]memory.MessageRecord,
) ([]memory.MessageRecord, error) {
	ordered := make([]memory.MessageRecord, 0, len(turn.Messages))
	for _, message := range turn.Messages {
		record, found := records[message.MessageID]
		if !found {
			return nil, turnIdentityConflict("legacy turn message identity differs")
		}

		if err := verifyTurnMessageRecord(ctx, txn, turn.SourceID, message, record, true); err != nil {
			return nil, err
		}

		ordered = append(ordered, record)
	}

	return ordered, nil
}

func loadCertifiedTurnMessages(ctx context.Context,
	txn *Tx,
	turn memory.Turn,
	forgotten bool) ([]memory.MessageRecord,
	error) {
	records := make([]memory.MessageRecord, 0, len(turn.Messages))
	for _, message := range turn.Messages {
		key, err := privacyKey(turn.Scope, Message, message.MessageID)
		if err != nil {
			return nil, err
		}

		var record memory.MessageRecord
		if err := getPrivacyRecord(ctx, txn, key, Message, &record); err != nil {
			return nil, proofStoreFailure("read certified turn message", err)
		}

		if err := verifyTurnMessageRecord(ctx, txn, turn.SourceID, message, record, !forgotten); err != nil {
			return nil, err
		}

		records = append(records, record)
	}

	return records, nil
}

func verifyTurnMessageRecord(
	ctx context.Context, txn *Tx,
	sourceID string,
	message memory.Message,
	record memory.MessageRecord,
	withPayload bool,
) error {
	if err := record.Validate(); err != nil {
		return proofStoreFailure("invalid canonical turn message", err)
	}

	if record.SourceID != sourceID || record.MessageID != message.MessageID || record.Role != message.Role {
		return turnIdentityConflict("canonical turn message identity differs")
	}

	if !withPayload {
		return nil
	}

	data, err := loadTurnIdentityPayload(ctx, txn, record.Payload)
	if err != nil {
		return err
	}

	if string(data) != message.Text {
		return turnIdentityConflict("canonical turn message authoritative content differs")
	}

	return nil
}
