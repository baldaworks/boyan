package badger

import (
	"encoding/json"
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func verifyLegacyTurn(
	txn *badger.Txn,
	turn memory.Turn,
	source memory.CanonicalSourceRecord,
) ([]memory.MessageRecord, bool, error) {
	data, err := loadTurnIdentityPayload(txn, source.Payload)
	if err != nil {
		return nil, false, err
	}

	fullInput, err := verifyLegacySourceData(turn, data)
	if err != nil {
		return nil, false, err
	}

	messages, err := scanLegacyTurnMessages(txn, turn)
	if err != nil {
		return nil, false, err
	}

	return messages, fullInput, nil
}

func verifyLegacySourceData(turn memory.Turn, data []byte) (bool, error) {
	var archived memory.SourceRecord
	if err := json.Unmarshal(data, &archived); err != nil {
		return false, eraseStoreFailure("decode legacy canonical source", err)
	}

	if archived.Turn != nil {
		if err := archived.Validate(); err != nil {
			return false, eraseStoreFailure("invalid legacy raw source", err)
		}

		if archived.Ref.Scope != turn.Scope || archived.Ref.ExportID != turn.ExportID {
			return false, turnIdentityConflict("legacy raw source identity differs")
		}

		return true, compareLegacyRawTurn(turn, *archived.Turn)
	}

	var raw memory.Turn
	if err := json.Unmarshal(data, &raw); err != nil {
		return false, eraseStoreFailure("decode legacy raw turn", err)
	}

	if raw.SchemaVersion != "" {
		return true, compareLegacyRawTurn(turn, raw)
	}

	return false, verifyLegacyNativeSource(turn, data)
}

func compareLegacyRawTurn(turn, stored memory.Turn) error {
	expected, err := memory.CanonicalTurnFingerprint(turn)
	if err != nil {
		return err
	}

	actual, err := memory.CanonicalTurnFingerprint(stored)
	if err != nil {
		return eraseStoreFailure("invalid legacy raw turn", err)
	}

	if expected != actual {
		return turnIdentityConflict("legacy turn authoritative content differs")
	}

	return nil
}

func verifyLegacyNativeSource(turn memory.Turn, data []byte) error {
	if turn.Conversation.LineageID != "" || turn.Conversation.PreviousConversationID != "" {
		return turnIdentityConflict("legacy native conversation lineage cannot be verified")
	}

	tools := 0

	for _, message := range turn.Messages {
		if message.Role == memory.MessageRoleTool {
			tools++
		}
	}

	if tools > 1 {
		return turnIdentityConflict("legacy native tool order cannot be verified")
	}

	return verifyNativeTurnSourceData(turn, data)
}

func verifyNativeTurnSourceData(turn memory.Turn, data []byte) error {
	var stored canonicalTurnSourcePayload
	if err := json.Unmarshal(data, &stored); err != nil {
		return eraseStoreFailure("decode native source identity", err)
	}

	status := stored.TerminalStatus
	if status == "" {
		status = memory.TurnTerminalStatusSuccess
	}

	if stored.ExportID != turn.ExportID || stored.ConversationID != turn.Conversation.ConversationID ||
		stored.AgentConversationID != turn.Conversation.AgentConversationID || stored.SourceTurnID != turn.SourceTurnID ||
		!stored.CompletedAt.Equal(turn.CompletedAt) || status != turn.TerminalStatus {
		return turnIdentityConflict("canonical source authoritative identity differs")
	}

	return nil
}

func loadTurnIdentityPayload(txn *badger.Txn, ref memory.PayloadRef) ([]byte, error) {
	data, err := listedCanonicalSourcePayload(txn, ref.ID)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil, turnIdentityConflict("canonical turn evidence is unavailable")
	}

	if err != nil {
		return nil, eraseStoreFailure("read canonical turn evidence", err)
	}

	if !isBadgerPayloadValid(data, ref) {
		return nil, eraseStoreFailure("canonical turn evidence is corrupt", nil)
	}

	return data, nil
}
