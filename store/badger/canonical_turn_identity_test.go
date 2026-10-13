package badger //nolint:testpackage // Tests serialized legacy evidence and private identity transactions.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func TestLegacyTurnIdentityProofAndAtomicCertificate(t *testing.T) {
	store, err := OpenCanonicalStore(t.TempDir())
	requireEraseTestNoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	turn, source, messages := seedPreCertificateTurn(t, store)
	verified, err := store.VerifyCanonicalTurn(context.Background(), turn)
	requireEraseTestNoError(t, err)

	if !verified.Found ||
		verified.Forgotten ||
		verified.Source.Payload != source.Payload ||
		len(verified.Messages) != len(messages) {
		t.Fatalf("proof=%+v", verified)
	}

	changed := turn
	changed.Messages = append([]memory.Message(nil), turn.Messages...)

	changed.Messages[0].Text = "changed authoritative content"
	if _, err := store.VerifyCanonicalTurn(context.Background(), changed); err == nil {
		t.Fatal("changed old source certified")
	}

	fingerprint, err := memory.CanonicalTurnFingerprint(turn)
	requireEraseTestNoError(t, err)

	certificate := memory.CanonicalTurnCertificate{
		Version:     memory.CanonicalTurnFingerprintVersion,
		SourceID:    turn.SourceID,
		Fingerprint: fingerprint,
	}
	for _, message := range turn.Messages {
		certificate.MessageIDs = append(certificate.MessageIDs, message.MessageID)
	}

	mutation := memory.CanonicalMutation{
		SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: turn.Scope,
		Operation: memory.OperationRecord{
			OperationID: "new-derivation",
			Fingerprint: "derivation-fingerprint",
			CommittedAt: turn.CompletedAt,
		},
		Sources: []memory.CanonicalSourceRecord{source}, Messages: messages,
		TurnIdentity: &memory.CanonicalTurnIdentityCommit{Certificate: certificate, Turn: turn},
	}
	_, err = store.ApplyCanonicalMutation(context.Background(), mutation)
	requireEraseTestNoError(t, err)

	changedMutation := mutation

	changedMutation.TurnIdentity = &memory.CanonicalTurnIdentityCommit{Certificate: certificate, Turn: changed}
	if _, err := store.ApplyCanonicalMutation(context.Background(), changedMutation); err == nil {
		t.Fatal("operation replay bypassed authoritative identity")
	}
}

func seedPreCertificateTurn(
	t *testing.T,
	store *CanonicalStore,
	tools ...memory.Message,
) (memory.Turn, memory.CanonicalSourceRecord, []memory.MessageRecord) {
	t.Helper()

	turn, err := memory.NewTerminalTurnWithTools(memory.Scope{Key: "test:legacy-identity", Kind: memory.ScopeKindPersonal},
		memory.ConversationRef{ConversationID: "conversation", AgentConversationID: "agent"}, "turn",
		time.Date(
			2026,
			time.August,
			5,
			12,
			0,
			0,
			0,
			time.FixedZone("legacy",
				6*60*60)),
		"original user text",
		"original assistant text",
		tools,
		memory.TurnTerminalStatusSuccess,
	)
	requireEraseTestNoError(t, err)
	data := serializedLegacyNativeSource(t, turn)

	source := memory.CanonicalSourceRecord{SourceID: turn.SourceID, Scope: turn.Scope,
		Sensitivity: memory.SensitivityStandard, Retention: memory.RetentionClassStandard, Payload: eraseTestPayload(
			"legacy-global-source",
			data,
		)}
	messages := make([]memory.MessageRecord, 0, len(turn.Messages))
	err = store.db.Update(func(txn *badger.Txn) error {
		if err := putSerializedLegacyRecord(txn, turn.Scope, badgerRecordSource, source.SourceID, source); err != nil {
			return err
		}

		if err := putSerializedLegacyPayload(txn, source.Payload, data); err != nil {
			return err
		}

		for _, message := range turn.Messages {
			record := memory.MessageRecord{MessageID: message.MessageID, SourceID: turn.SourceID, Role: message.Role,
				Payload: eraseTestPayload("legacy-global-"+message.MessageID, []byte(message.Text))}
			if err := putSerializedLegacyRecord(txn, turn.Scope, badgerRecordMessage, record.MessageID, record); err != nil {
				return err
			}

			if err := putSerializedLegacyPayload(txn, record.Payload, []byte(message.Text)); err != nil {
				return err
			}

			messages = append(messages, record)
		}

		return nil
	})
	requireEraseTestNoError(t, err)

	return turn, source, messages
}

func putSerializedLegacyRecord(txn *badger.Txn, scope memory.Scope, kind, id string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	encoded, err := memory.MarshalRecordEnvelope(kind, data)
	if err != nil {
		return err
	}

	key, err := canonicalKey(scope, kind, id)
	if err != nil {
		return err
	}

	return txn.Set(key, encoded)
}

func putSerializedLegacyPayload(txn *badger.Txn, ref memory.PayloadRef, data []byte) error {
	return putSerializedLegacyRecord(txn, payloadScope(), badgerRecordPayload, ref.ID, data)
}

func serializedLegacyNativeSource(t *testing.T, turn memory.Turn) []byte {
	t.Helper()
	// These are the native source payload fields written before certificates existed.
	data, err := json.Marshal(map[string]any{
		"export_id": turn.ExportID, "conversation_id": turn.Conversation.ConversationID,
		"agent_conversation_id": turn.Conversation.AgentConversationID, "source_turn_id": turn.SourceTurnID,
		"completed_at": turn.CompletedAt, "terminal_status": turn.TerminalStatus,
	})
	requireEraseTestNoError(t, err)

	return data
}
