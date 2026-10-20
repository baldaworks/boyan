//go:build integration

package storefixture_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"testing"
	"time"
)

type privacyHistory struct {
	scope    memory.Scope
	source   memory.CanonicalSourceRecord
	messages []memory.MessageRecord
	item     memory.MemoryItem
	revision memory.MemoryRevision
	payloads []memory.CanonicalPayload
	turn     memory.Turn
}

func privacyRef(id string, data []byte) memory.PayloadRef {
	digest := sha256.Sum256(data)

	return memory.PayloadRef{ID: id, Digest: hex.EncodeToString(digest[:]), ByteSize: uint32(len(data))}
}

func newPrivacyHistory(t *testing.T) privacyHistory {
	t.Helper()

	scope := memory.Scope{Key: "test:privacy:" + t.Name(), Kind: memory.ScopeKindPersonal}

	turn, err := memory.NewTurn(scope,
		memory.ConversationRef{ConversationID: "conversation",
			AgentConversationID: "agent"},
		"turn",
		time.Date(2026,
			1,
			1,
			0,
			0,
			0,
			0,
			time.UTC),
		"remember canonical",
		"canonical result")
	if err != nil {
		t.Fatal(err)
	}

	header := privacyHistoryHeader(t, turn)

	h := privacyHistory{scope: scope, turn: turn}
	h.source = memory.CanonicalSourceRecord{SourceID: turn.SourceID,
		Scope: scope,
		Payload: privacyRef("source:"+t.Name(),
			header),
		Sensitivity: memory.SensitivityStandard,
		Retention:   memory.RetentionClassStandard}
	h.payloads = append(h.payloads, memory.CanonicalPayload{Ref: h.source.Payload, Data: header})

	for _, message := range turn.Messages {
		ref := privacyRef(message.MessageID, []byte(message.Text))
		h.messages = append(h.messages,
			memory.MessageRecord{MessageID: message.MessageID,
				SourceID: turn.SourceID,
				Role:     message.Role,
				Payload:  ref})
		h.payloads = append(h.payloads, memory.CanonicalPayload{Ref: ref, Data: []byte(message.Text)})
	}

	h.item = memory.MemoryItem{ItemID: "item", Scope: scope, Kind: memory.MemoryKindState, MemoryKey: "key"}

	setPrivacyHistoryRevision(t, &h)

	h.payloads = append(h.payloads, memory.CanonicalPayload{Ref: h.revision.Payload, Data: []byte("protected memory")})
	for _, err := range []error{h.source.Validate(), h.item.Validate(), h.revision.Validate()} {
		if err != nil {
			t.Fatal(err)
		}
	}

	return h
}

func privacyHistoryHeader(t *testing.T, turn memory.Turn) []byte {
	t.Helper()

	header, err := json.Marshal(map[string]any{"export_id": turn.ExportID,
		"conversation_id":       turn.Conversation.ConversationID,
		"agent_conversation_id": turn.Conversation.AgentConversationID,
		"source_turn_id":        turn.SourceTurnID,
		"completed_at":          turn.CompletedAt,
		"terminal_status":       turn.TerminalStatus})
	if err != nil {
		t.Fatal(err)
	}

	return header
}

func setPrivacyHistoryRevision(t *testing.T, h *privacyHistory) {
	t.Helper()

	evidence, err := memory.NewEvidenceRef(h.turn.SourceID,
		h.turn.Messages[0].MessageID,
		memory.MessageRoleUser,
		h.turn.Messages[0].Text,
		0,
		uint32(len(h.turn.Messages[0].Text)),
		memory.AssertionModeUser)
	if err != nil {
		t.Fatal(err)
	}

	h.revision = memory.MemoryRevision{SchemaVersion: memory.MemorySchemaVersion,
		RevisionID:  "revision",
		ItemID:      "item",
		Revision:    1,
		Temporal:    memory.Temporal{ObservedAt: h.turn.CompletedAt},
		Evidence:    []memory.EvidenceRef{evidence},
		Sensitivity: memory.SensitivityStandard,
		Retention:   memory.RetentionClassStandard}
	h.revision.Payload = privacyRef("revision", []byte("protected memory"))
}

func nativeMutation(t *testing.T) memory.CanonicalMutation {
	h := newPrivacyHistory(t)

	return memory.CanonicalMutation{SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: h.scope,
		Operation: memory.OperationRecord{OperationID: "initial", Fingerprint: "initial",
			CommittedAt: h.turn.CompletedAt}, Sources: []memory.CanonicalSourceRecord{h.source}, Messages: h.messages,
		Items: []memory.MemoryItem{h.item}, Revisions: []memory.MemoryRevision{h.revision},
		Heads: []memory.ItemHead{{ItemID: h.item.ItemID, RevisionID: h.revision.RevisionID}}, Payloads: h.payloads}
}
