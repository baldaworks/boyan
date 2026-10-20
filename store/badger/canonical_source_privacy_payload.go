package badger

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// Classification must bind the raw representation that trace will return.
// A different message table cannot certify an embedded Turn's text.
func readPrivacySourcePayload(txn *badger.Txn, source memory.CanonicalSourceRecord) ([]byte, error) {
	if source.Payload.ByteSize > maxCanonicalValueBytes {
		return nil, invalidSourcePrivacy()
	}

	key, err := payloadRecordKey(source.Payload)
	if err != nil {
		return nil, err
	}

	var payload []byte

	err = getPrivacyPreparationRecord(txn, key, badgerRecordPayload, &payload)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	if (memory.CanonicalPayload{Ref: source.Payload, Data: payload}).Validate() != nil {
		return nil, invalidSourcePrivacy()
	}

	return payload, nil
}

func validatePrivacySourcePayload(txn *badger.Txn, source memory.CanonicalSourceRecord) (bool, error) {
	payload, err := readPrivacySourcePayload(txn, source)
	if err != nil || payload == nil {
		return false, err
	}

	migrated, recognized, err := migratedTraceSourceRecord(payload, source.Scope)
	if recognized {
		if err != nil {
			return false, invalidSourcePrivacy()
		}

		return validatePrivacyEmbeddedTurn(txn, source, migrated)
	}

	return validPrivacyNativePayload(payload, source.Scope), nil
}

func validPrivacyNativePayload(payload []byte, scope memory.Scope) bool {
	var native canonicalTurnSourcePayload
	if decodeCanonicalProofJSON(payload, &native) != nil {
		return false
	}

	ref := memory.SourceRef{Scope: scope, ExportID: native.ExportID,
		ConversationID: native.ConversationID, SourceTurnID: native.SourceTurnID}
	if ref.Validate() != nil || native.AgentConversationID == "" || native.CompletedAt.IsZero() {
		return false
	}

	return native.TerminalStatus == memory.TurnTerminalStatusSuccess ||
		native.TerminalStatus == memory.TurnTerminalStatusFailed || native.TerminalStatus == ""
}

func validatePrivacyRawEvidence(
	txn *badger.Txn,
	source memory.CanonicalSourceRecord,
	ref memory.EvidenceRef,
) (bool, error) {
	payload, err := readPrivacySourcePayload(txn, source)
	if err != nil || payload == nil {
		return false, err
	}

	record, migrated, err := migratedTraceSourceRecord(payload, source.Scope)
	if !migrated {
		return validPrivacyNativePayload(payload, source.Scope), nil
	}

	if err != nil {
		return false, invalidSourcePrivacy()
	}

	if record.Turn == nil {
		return false, nil
	}

	for _, message := range record.Turn.Messages {
		if message.MessageID == ref.MessageID {
			digest := sha256.Sum256([]byte(message.Text))

			return message.Role == ref.Role && ref.EndByte <= uint32(len(message.Text)) &&
				hex.EncodeToString(digest[:]) == ref.TextDigest, nil
		}
	}

	return false, nil
}

func validatePrivacyEmbeddedTurn(
	txn *badger.Txn,
	source memory.CanonicalSourceRecord,
	record memory.SourceRecord,
) (bool, error) {
	if record.Turn == nil {
		return false, nil
	}

	if source.SourceID != record.Turn.SourceID && source.SourceID != record.Ref.ExportID {
		return false, invalidSourcePrivacy()
	}

	complete := true

	for _, message := range record.Turn.Messages {
		valid, err := validatePrivacyEmbeddedMessage(txn, source, message)
		if err != nil {
			return false, err
		}

		complete = complete && valid
	}

	return complete, nil
}

func validatePrivacyEmbeddedMessage(
	txn *badger.Txn,
	source memory.CanonicalSourceRecord,
	message memory.Message,
) (bool, error) {
	key, err := canonicalKey(source.Scope, badgerRecordMessage, message.MessageID)
	if err != nil {
		return false, err
	}

	var stored memory.MessageRecord

	err = getCanonicalRecord(txn, key, badgerRecordMessage, &stored)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	digest := sha256.Sum256([]byte(message.Text))
	if stored.SourceID != source.SourceID || stored.Role != message.Role ||
		stored.Payload.ByteSize != uint32(len(message.Text)) || stored.Payload.Digest != hex.EncodeToString(digest[:]) {
		return false, invalidSourcePrivacy()
	}

	return true, nil
}

func hasPrivacySourceHistory(txn *badger.Txn, scope memory.Scope, sourceID string) (bool, error) {
	prefix, err := badgerProvenancePrefix(scope, badgerRecordSourceRevision, sourceID)
	if err != nil {
		return false, err
	}

	options := badger.DefaultIteratorOptions
	options.Prefix = prefix
	options.PrefetchValues = false

	iterator := txn.NewIterator(options)
	defer iterator.Close()

	iterator.Seek(prefix)

	return iterator.ValidForPrefix(prefix), nil
}
