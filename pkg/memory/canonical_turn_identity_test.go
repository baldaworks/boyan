package memory_test

import (
	"testing"
	"time"

	m "github.com/baldaworks/boyan/pkg/memory"
)

func identityTestTurn(t *testing.T) m.Turn {
	t.Helper()

	turn, err := m.NewTerminalTurnWithTools(
		m.Scope{Key: "test:identity", Kind: m.ScopeKindPersonal},
		m.ConversationRef{ConversationID: "conversation", AgentConversationID: "agent"},
		"turn", time.Date(2026, time.October, 13, 1, 2, 3, 0, time.UTC), "user", "assistant",
		[]m.Message{{ToolName: "first", ToolCallID: "call-1", Text: "first output"},
			{ToolName: "second", ToolCallID: "call-2", Text: "second output"}}, m.TurnTerminalStatusSuccess,
	)
	if err != nil {
		t.Fatal(err)
	}

	return turn
}

func TestCanonicalTurnIdentityNormalizesEquivalentRepresentations(t *testing.T) {
	t.Parallel()
	original := identityTestTurn(t)
	equivalent := original
	equivalent.Messages = append([]m.Message(nil), original.Messages...)
	equivalent.SourceID = ""
	equivalent.Messages[0].MessageID = ""
	equivalent.Messages[1].MessageID = ""
	equivalent.TerminalStatus = ""
	equivalent.CompletedAt = original.CompletedAt.In(time.FixedZone("offset", 6*60*60))

	expected, err := m.CanonicalTurnFingerprint(original)
	if err != nil {
		t.Fatal(err)
	}

	actual, err := m.CanonicalTurnFingerprint(equivalent)
	if err != nil {
		t.Fatal(err)
	}

	if actual != expected {
		t.Fatal("equivalent terminal exports have different identities")
	}

	normalized, err := m.NormalizeCanonicalTurn(equivalent)
	if err != nil {
		t.Fatal(err)
	}

	if normalized.Messages[0].MessageID == "" || equivalent.Messages[0].MessageID != "" {
		t.Fatal("normalization either omitted generated IDs or changed caller messages")
	}

	if normalized.TerminalStatus != m.TurnTerminalStatusSuccess || normalized.CompletedAt.Location() != time.UTC {
		t.Fatal("normalization did not canonicalize terminal status and UTC")
	}
}

func TestCanonicalTurnIdentityBindsAuthoritativeFieldsAndToolOrder(t *testing.T) {
	t.Parallel()
	original := identityTestTurn(t)

	expected, err := m.CanonicalTurnFingerprint(original)
	if err != nil {
		t.Fatal(err)
	}

	changes := map[string]func(*m.Turn){
		"scope kind":            func(turn *m.Turn) { turn.Scope.Kind = m.ScopeKindGroup },
		"lineage":               func(turn *m.Turn) { turn.Conversation.LineageID = "other-lineage" },
		"previous conversation": func(turn *m.Turn) { turn.Conversation.PreviousConversationID = "previous" },
		"agent conversation":    func(turn *m.Turn) { turn.Conversation.AgentConversationID = "other-agent" },
		"completed time":        func(turn *m.Turn) { turn.CompletedAt = turn.CompletedAt.Add(time.Nanosecond) },
		"terminal status":       func(turn *m.Turn) { turn.TerminalStatus = m.TurnTerminalStatusFailed },
		"user text":             func(turn *m.Turn) { turn.Messages[0].Text = "changed user" },
		"assistant text":        func(turn *m.Turn) { turn.Messages[1].Text = "changed assistant" },
		"tool text":             func(turn *m.Turn) { turn.Messages[2].Text = "changed tool" },
		"tool order": func(turn *m.Turn) {
			turn.Messages[2], turn.Messages[3] = turn.Messages[3], turn.Messages[2]
		},
		"tool name": func(turn *m.Turn) {
			turn.Messages[2].ToolName = "changed"
			turn.Messages[2].MessageID = m.TurnToolMessageID(turn.ExportID, "changed", turn.Messages[2].ToolCallID)
		},
		"tool call": func(turn *m.Turn) {
			turn.Messages[2].ToolCallID = "changed"
			turn.Messages[2].MessageID = m.TurnToolMessageID(turn.ExportID, turn.Messages[2].ToolName, "changed")
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			turn := original
			turn.Messages = append([]m.Message(nil), original.Messages...)
			change(&turn)

			actual, err := m.CanonicalTurnFingerprint(turn)
			if err != nil {
				t.Fatal(err)
			}

			if actual == expected {
				t.Fatal("changed authoritative input retained the original identity")
			}
		})
	}
}

func TestCanonicalTurnIdentityBindsStableEnvelopeIdentity(t *testing.T) {
	t.Parallel()
	original := identityTestTurn(t)

	expected, err := m.CanonicalTurnFingerprint(original)
	if err != nil {
		t.Fatal(err)
	}

	changes := []string{"scope key", "conversation", "source turn"}
	for _, change := range changes {
		t.Run(change, func(t *testing.T) {
			scope, conversation, sourceTurnID := original.Scope, original.Conversation, original.SourceTurnID

			switch change {
			case "scope key":
				scope.Key = "test:other"
			case "conversation":
				conversation.ConversationID = "other-conversation"
			case "source turn":
				sourceTurnID = "other-turn"
			}

			next, err := m.NewTerminalTurnWithTools(scope, conversation, sourceTurnID,
				original.CompletedAt, original.Messages[0].Text, original.Messages[1].Text,
				original.Messages[2:], original.TerminalStatus)
			if err != nil {
				t.Fatal(err)
			}

			actual, err := m.CanonicalTurnFingerprint(next)
			if err != nil {
				t.Fatal(err)
			}

			if actual == expected {
				t.Fatal("changed stable envelope retained original content identity")
			}
		})
	}
}
