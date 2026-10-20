//go:build integration

package canonicalsql

import (
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestProofLegacyToolNormalizationAndOrder(t *testing.T) {
	for _, kind := range []string{"single-normalized", "native-ambiguous", "embedded-ordered"} {
		t.Run(kind, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
				h := toolPrivacyHistory(t, kind)
				if kind == "embedded-ordered" {
					setProofRawTurn(t, &h, "embedded-turn")
				}

				seedPrivacyHistory(t, owner, h)

				turn := h.turn
				if kind == "single-normalized" {
					turn.SourceID, turn.TerminalStatus = "", ""
					turn.CompletedAt = turn.CompletedAt.UTC()
					turn.Messages[0].MessageID, turn.Messages[1].MessageID = "", ""
				}

				proof, err := owner.VerifyCanonicalTurn(t.Context(), turn)
				if kind == "native-ambiguous" {
					assertCode(t, err, memory.CodeConflict)

					return
				}

				requireProofSuccess(t, err)

				if !proof.Found {
					t.Fatal("valid tool turn was not found")
				}

				if kind == "embedded-ordered" {
					turn.Messages[2], turn.Messages[3] = turn.Messages[3], turn.Messages[2]
					_, err = owner.VerifyCanonicalTurn(t.Context(), turn)
					assertCode(t, err, memory.CodeConflict)
				}
			})
		})
	}
}

func toolPrivacyHistory(t *testing.T, kind string) privacyHistory {
	t.Helper()

	h := newPrivacyHistory(t)

	tools := []memory.Message{{Role: memory.MessageRoleTool, ToolName: "lookup",
		ToolCallID: "first", Text: "first result"}}
	if kind != "single-normalized" {
		tools = append(tools, memory.Message{Role: memory.MessageRoleTool, ToolName: "lookup",
			ToolCallID: "second", Text: "second result"})
	}

	turn, err := memory.NewTerminalTurnWithTools(h.scope, h.turn.Conversation, h.turn.SourceTurnID,
		h.turn.CompletedAt, h.turn.Messages[0].Text, h.turn.Messages[1].Text, tools, memory.TurnTerminalStatusSuccess)
	requireProofSuccess(t, err)

	h.turn = turn
	header := privacyHistoryHeader(t, turn)
	h.source.Payload = privacyRef("source", header)
	h.messages = nil
	h.payloads = []memory.CanonicalPayload{{Ref: h.source.Payload, Data: header}}

	for _, message := range turn.Messages {
		ref := privacyRef(message.MessageID, []byte(message.Text))
		h.messages = append(h.messages, memory.MessageRecord{MessageID: message.MessageID,
			SourceID: turn.SourceID, Role: message.Role, Payload: ref})
		h.payloads = append(h.payloads, memory.CanonicalPayload{Ref: ref, Data: []byte(message.Text)})
	}

	h.payloads = append(h.payloads, memory.CanonicalPayload{Ref: h.revision.Payload, Data: []byte("protected memory")})

	return h
}
