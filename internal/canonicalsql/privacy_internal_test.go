//go:build integration

package canonicalsql

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

type privacyAPI interface {
	PrepareSourcePrivacy(ctx context.Context) error
	SourcePrivacyReady(ctx context.Context) error
}

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

	scope := memory.Scope{Key: "test:privacy", Kind: memory.ScopeKindPersonal}

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
		Payload: privacyRef("source",
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

func seedPrivacyHistory(t *testing.T, owner *Owner, h privacyHistory) {
	t.Helper()

	err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		put := func(f Family, id string, value any) error {
			return tx.Put(ctx, Key{Family: f, Scope: h.scope, Parts: [][]byte{[]byte(id)}}, envelope(t, f, value))
		}
		if err := put(Source, h.source.SourceID, h.source); err != nil {
			return err
		}

		if err := put(Item, h.item.ItemID, h.item); err != nil {
			return err
		}

		if err := put(Revision, h.revision.RevisionID, h.revision); err != nil {
			return err
		}

		for _, message := range h.messages {
			if err := put(Message, message.MessageID, message); err != nil {
				return err
			}
		}

		for _, payload := range h.payloads {
			if err := tx.Put(ctx,
				Key{Family: Payload,
					Parts: [][]byte{[]byte(payload.Ref.ID)}},
				envelope(t,
					Payload,
					payload.Data)); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPrivacyPreparationHistory(t *testing.T) {
	for _, kind := range []string{"standard",
		"sensitive",
		"opaque",
		"missing-message",
		"missing-blob",
		"missing-item",
		"incomplete-evidence",
		"identity-only",
		"unsupported"} {
		t.Run(kind, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, reopen func() *Owner) {
				runPrivacyHistory(t, owner, reopen, kind)
			})
		})
	}
}

func runPrivacyHistory(t *testing.T, owner *Owner, reopen func() *Owner, kind string) {
	t.Helper()
	h := newPrivacyHistory(t)
	want := configurePrivacyCase(&h, kind)

	seedPrivacyHistory(t, owner, h)

	if kind == "missing-item" || kind == "identity-only" {
		family, id := Item, h.item.ItemID
		if kind == "identity-only" {
			family, id = Revision, h.revision.RevisionID
		}

		if err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
			return tx.Delete(ctx, Key{Family: family, Scope: h.scope, Parts: [][]byte{[]byte(id)}})
		}); err != nil {
			t.Fatal(err)
		}
	}

	api, ok := any(owner).(privacyAPI)
	if !ok {
		t.Fatal("SQL owner lacks privacy preparation authority")
	}

	assertCode(t, api.SourcePrivacyReady(t.Context()), memory.CodeUnavailable)

	if err := api.PrepareSourcePrivacy(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}

	owner = reopen()
	if err := any(owner).(privacyAPI).SourcePrivacyReady(t.Context()); err != nil {
		t.Fatal(err)
	}

	assertPrivacyHistory(t, owner, h, want)
}
func configurePrivacyCase(h *privacyHistory, kind string) string {
	want := "unknown"

	switch kind {
	case "standard":
		want = "standard"
	case "sensitive":
		h.revision.Sensitivity = memory.SensitivitySensitive
		want = "sensitive"
	case "opaque":
		h.payloads[0].Data = []byte("opaque")
		h.payloads[0].Ref = privacyRef("source", h.payloads[0].Data)
		h.source.Payload = h.payloads[0].Ref
	case "missing-message":
		h.messages = h.messages[1:]
	case "missing-blob":
		h.payloads = h.payloads[:1]
	case "incomplete-evidence":
		h.revision.Evidence[0].TextDigest = ""
	case "unsupported":
		h.revision.SchemaVersion = "unsupported"
	}

	return want
}
func assertPrivacyHistory(t *testing.T, owner *Owner, h privacyHistory, want string) {
	t.Helper()

	record := privacyRecordState(t, owner, h)
	if record.State != want || record.Preparing || len(record.MessageIDs) > 32 {
		t.Fatalf("classification=%+v want %s", record, want)
	}

	if err := owner.CheckIntegrity(t.Context()); err != nil {
		t.Fatal(err)
	}
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
