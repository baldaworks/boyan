package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// Old source records contain no ordered membership manifest. Scan the exact
// scope only for compatibility proof, retaining at most one bounded turn.
func scanLegacyTurnMessages(txn *badger.Txn, turn memory.Turn) ([]memory.MessageRecord, error) {
	prefix, err := canonicalPrefix(turn.Scope, badgerRecordMessage)
	if err != nil {
		return nil, err
	}

	iterator := txn.NewIterator(badger.DefaultIteratorOptions)
	defer iterator.Close()

	records := make(map[string]memory.MessageRecord, len(turn.Messages))

	for iterator.Seek(prefix); iterator.ValidForPrefix(prefix); iterator.Next() {
		var record memory.MessageRecord
		if err := getCanonicalRecord(txn, iterator.Item().Key(), badgerRecordMessage, &record); err != nil {
			return nil, eraseStoreFailure("decode legacy message", err)
		}

		if record.SourceID != turn.SourceID {
			continue
		}

		if err := record.Validate(); err != nil {
			return nil, eraseStoreFailure("invalid legacy source message", err)
		}

		if len(records) >= maxCanonicalTraceSourceMessages {
			return nil, turnIdentityConflict("legacy turn message membership is not bounded")
		}

		if _, exists := records[record.MessageID]; exists {
			return nil, eraseStoreFailure("duplicate legacy source message", nil)
		}

		records[record.MessageID] = record
	}

	if len(records) != len(turn.Messages) {
		return nil, turnIdentityConflict("legacy turn message membership differs")
	}

	return compareLegacyTurnMessages(txn, turn, records)
}

func compareLegacyTurnMessages(
	txn *badger.Txn,
	turn memory.Turn,
	records map[string]memory.MessageRecord,
) ([]memory.MessageRecord, error) {
	ordered := make([]memory.MessageRecord, 0, len(turn.Messages))
	for _, message := range turn.Messages {
		record, found := records[message.MessageID]
		if !found {
			return nil, turnIdentityConflict("legacy turn message identity differs")
		}

		if err := verifyTurnMessageRecord(txn, turn.SourceID, message, record, true); err != nil {
			return nil, err
		}

		ordered = append(ordered, record)
	}

	return ordered, nil
}

func loadCertifiedTurnMessages(txn *badger.Txn, turn memory.Turn, forgotten bool) ([]memory.MessageRecord, error) {
	records := make([]memory.MessageRecord, 0, len(turn.Messages))
	for _, message := range turn.Messages {
		key, err := canonicalKey(turn.Scope, badgerRecordMessage, message.MessageID)
		if err != nil {
			return nil, err
		}

		var record memory.MessageRecord
		if err := getCanonicalRecord(txn, key, badgerRecordMessage, &record); err != nil {
			return nil, eraseStoreFailure("read certified turn message", err)
		}

		if err := verifyTurnMessageRecord(txn, turn.SourceID, message, record, !forgotten); err != nil {
			return nil, err
		}

		records = append(records, record)
	}

	return records, nil
}

func verifyTurnMessageRecord(
	txn *badger.Txn,
	sourceID string,
	message memory.Message,
	record memory.MessageRecord,
	withPayload bool,
) error {
	if err := record.Validate(); err != nil {
		return eraseStoreFailure("invalid canonical turn message", err)
	}

	if record.SourceID != sourceID || record.MessageID != message.MessageID || record.Role != message.Role {
		return turnIdentityConflict("canonical turn message identity differs")
	}

	if !withPayload {
		return nil
	}

	data, err := loadTurnIdentityPayload(txn, record.Payload)
	if err != nil {
		return err
	}

	if string(data) != message.Text {
		return turnIdentityConflict("canonical turn message authoritative content differs")
	}

	return nil
}
