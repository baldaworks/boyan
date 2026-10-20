package canonicalsql

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestProofTurnEvidenceRestrictions(t *testing.T) {
	for _, kind := range []string{"normalized",
		"missing-payload",
		"corrupt-payload",
		"legacy-denied",
		"certified-denied",
		"missing-source",
		"missing-message",
		"copied-scope",
		"copied-id",
		"tampered-message",
		"legacy-export",
		"denied-export",
		"raw-turn",
		"embedded-turn",
		"lineage",
		"certified-extension",
		"certified-source-change"} {
		t.Run(kind, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) { runProofTurnCase(t, owner, kind) })
		})
	}
}

func runProofTurnCase(t *testing.T, owner *Owner, kind string) {
	t.Helper()

	h := newPrivacyHistory(t)
	setProofRawTurn(t, &h, kind)

	mutation := proofTurnMutation(t, h)
	seedPrivacyHistory(t, owner, h)

	certified := slices.Contains([]string{"certified-denied", "missing-source", "missing-message",
		"copied-scope", "copied-id", "tampered-message", "certified-extension", "certified-source-change"}, kind)

	err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		if certified {
			if err := persistTurnCertificate(ctx, tx, mutation); err != nil {
				return err
			}
		}

		return alterProofTurnFixture(ctx, tx, h, kind)
	})
	requireProofSuccess(t, err)

	turn := h.turn
	if kind == "normalized" {
		turn.SourceID = ""
		turn.TerminalStatus = ""

		turn.Messages = slices.Clone(turn.Messages)
		for i := range turn.Messages {
			turn.Messages[i].MessageID = ""
		}
	}

	if kind == "lineage" {
		turn.Conversation.LineageID = "new-lineage"
	}

	if kind == "copied-scope" {
		turn.Scope.Kind = memory.ScopeKindGroup
	}

	if assertCertifiedMutationRejected(t, owner, mutation, kind) {
		return
	}

	result, err := owner.VerifyCanonicalTurn(t.Context(), turn)

	assertProofTurnResult(t, result, err, kind)
}

func alterProofTurnFixture(ctx context.Context, tx *Tx, h privacyHistory, kind string) error {
	put := func(f Family, id string, value any) error {
		key := Key{Family: f, Scope: h.scope, Parts: [][]byte{[]byte(id)}}

		return putPrivacyRecord(ctx, tx, key, f, value)
	}
	remove := func(f Family, id string) error {
		key := Key{Family: f, Scope: h.scope, Parts: [][]byte{[]byte(id)}}

		return tx.Delete(ctx, key)
	}

	switch kind {
	case "missing-payload", "legacy-denied", "certified-denied":
		key, _ := payloadRecordKey(h.source.Payload)
		if err := tx.Delete(ctx, key); err != nil {
			return err
		}

		if kind != "missing-payload" {
			return put(DeniedSource, h.source.SourceID, deniedSource{SourceID: h.source.SourceID, DeniedAt: h.turn.CompletedAt})
		}
	case "corrupt-payload":
		key, _ := payloadRecordKey(h.source.Payload)

		return putPrivacyRecord(ctx, tx, key, Payload, []byte("corrupt"))
	case "missing-source":
		return remove(Source, h.source.SourceID)
	case "missing-message":
		return remove(Message, h.messages[0].MessageID)
	case "tampered-message":
		message := h.messages[0]
		message.Role = memory.MessageRoleAssistant

		return put(Message, message.MessageID, message)
	default:
		return alterProofTurnIdentity(ctx, tx, h, kind, put, remove)
	}

	return nil
}

func setProofRawTurn(t *testing.T, h *privacyHistory, kind string) {
	t.Helper()

	if kind == "raw-turn" || kind == "embedded-turn" {
		var value any = h.turn
		if kind == "embedded-turn" {
			value = memory.SourceRecord{SchemaVersion: memory.DerivedSchemaVersionV1,
				State: memory.SourceStateActive,
				Turn:  &h.turn,
				Ref: memory.SourceRef{Scope: h.scope,
					ExportID:       h.turn.ExportID,
					ConversationID: h.turn.Conversation.ConversationID,
					SourceTurnID:   h.turn.SourceTurnID}}
		}

		data, err := json.Marshal(value)
		requireProofSuccess(t, err)

		h.source.Payload = privacyRef("source", data)
		h.payloads[0] = memory.CanonicalPayload{Ref: h.source.Payload, Data: data}
	}
}

func assertProofTurnResult(t *testing.T, result memory.CanonicalTurnIdentityResult, err error, kind string) {
	t.Helper()

	switch kind {
	case "normalized", "raw-turn", "embedded-turn", "certified-denied":
		assertSuccessfulTurnProof(t, result, err, kind)
	case "copied-scope":
		if err == nil && result.FullInputVerified {
			t.Fatal("copied proof authorized foreign scope")
		}
	case "corrupt-payload":
		assertCode(t, err, memory.CodeStoreFailure)
	case "missing-source", "missing-message", "copied-id":
		if err == nil {
			t.Fatal("corrupt proof accepted")
		}
	default:
		assertCode(t, err, memory.CodeConflict)
	}
}

func alterProofTurnIdentity(ctx context.Context, tx *Tx, h privacyHistory, kind string,
	put func(Family, string, any) error, remove func(Family, string) error) error {
	switch kind {
	case "legacy-export", "denied-export":
		if err := remove(Source, h.source.SourceID); err != nil {
			return err
		}

		if kind == "denied-export" {
			return put(DeniedSource, h.turn.ExportID, deniedSource{SourceID: h.turn.ExportID, DeniedAt: h.turn.CompletedAt})
		}

		source := h.source
		source.SourceID = h.turn.ExportID

		return put(Source, source.SourceID, source)
	case "copied-scope":
		scope := h.scope
		scope.Kind = memory.ScopeKindGroup
		key, _ := privacyKey(scope, TurnCertificate, h.source.SourceID)
		original, _ := privacyKey(h.scope, TurnCertificate, h.source.SourceID)

		data, err := tx.Get(ctx, original)
		if err != nil {
			return err
		}

		return tx.Put(ctx, key, data)
	case "copied-id":
		cert, _, err := loadTurnCertificate(ctx, tx, h.scope, h.source.SourceID)
		if err != nil {
			return err
		}

		cert.SourceID = "other-source"

		return put(TurnCertificate, h.source.SourceID, cert)
	}

	return nil
}

func assertSuccessfulTurnProof(t *testing.T, result memory.CanonicalTurnIdentityResult, err error, kind string) {
	t.Helper()
	requireProofSuccess(t, err)

	switch kind {
	case "normalized":
		if !result.Found || result.FullInputVerified {
			t.Fatalf("normalization: %+v %v", result, err)
		}
	case "raw-turn", "embedded-turn":
		if !result.FullInputVerified {
			t.Fatalf("raw proof: %+v %v", result, err)
		}
	case "certified-denied":
		if !result.Forgotten || !result.FullInputVerified {
			t.Fatalf("forgotten proof: %+v %v", result, err)
		}
	}
}

func assertCertifiedMutationRejected(t *testing.T, owner *Owner, mutation memory.CanonicalMutation, kind string) bool {
	t.Helper()

	if kind == "certified-extension" || kind == "certified-source-change" {
		mutation.TurnIdentity = nil
		if kind == "certified-extension" {
			mutation.Messages = slices.Clone(mutation.Messages)
			mutation.Messages[0].MessageID = "new-message"
		} else {
			mutation.Sources = slices.Clone(mutation.Sources)
			mutation.Sources[0].Retention = memory.RetentionClassEphemeral
		}

		err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
			_, err := prepareTurnIdentityMutation(ctx, tx, mutation)

			return err
		})
		assertCode(t, err, memory.CodeConflict)

		return true
	}

	return false
}
