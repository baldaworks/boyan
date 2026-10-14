package memory_test

import (
	"testing"
	"time"

	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestTurnDeliveryFingerprintUsesCanonicalAuthority(t *testing.T) {
	original := identityTestTurn(t)

	changes := map[string]func(*m.Turn){
		"UTC offset": func(turn *m.Turn) {
			turn.CompletedAt = turn.CompletedAt.In(time.FixedZone("offset", 6*60*60))
		},
		"default status":     func(turn *m.Turn) { turn.TerminalStatus = "" },
		"generated source":   func(turn *m.Turn) { turn.SourceID = "" },
		"generated messages": func(turn *m.Turn) { turn.Messages[0].MessageID = ""; turn.Messages[1].MessageID = "" },
		"changed text":       func(turn *m.Turn) { turn.Messages[0].Text = "changed" },
		"changed order": func(turn *m.Turn) {
			turn.Messages[2], turn.Messages[3] = turn.Messages[3], turn.Messages[2]
		},
		"changed role":         func(turn *m.Turn) { turn.Messages[0].Role = m.MessageRoleAssistant },
		"changed instant":      func(turn *m.Turn) { turn.CompletedAt = turn.CompletedAt.Add(time.Nanosecond) },
		"changed status":       func(turn *m.Turn) { turn.TerminalStatus = m.TurnTerminalStatusFailed },
		"changed conversation": func(turn *m.Turn) { turn.Conversation.ConversationID = "another" },
		"changed lineage":      func(turn *m.Turn) { turn.Conversation.LineageID = "another" },
		"changed previous":     func(turn *m.Turn) { turn.Conversation.PreviousConversationID = "another" },
		"changed agent":        func(turn *m.Turn) { turn.Conversation.AgentConversationID = "another" },
		"changed scope key":    func(turn *m.Turn) { turn.Scope.Key = "another" },
		"changed scope kind":   func(turn *m.Turn) { turn.Scope.Kind = m.ScopeKindGroup },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			turn := original
			turn.Messages = append([]m.Message(nil), original.Messages...)
			change(&turn)
			assertTurnTransportCanonicalAuthority(t, turn)
		})
	}
}

func assertTurnTransportCanonicalAuthority(t *testing.T, turn m.Turn) {
	t.Helper()

	canonical, canonicalErr := m.CanonicalTurnFingerprint(turn)
	envelope := m.IngestEnvelope[m.Turn]{SchemaVersion: m.IngestHTTPSchemaVersionV1,
		ProducerID: "producer", ScopeSequence: 1, Payload: turn}

	transport, err := m.TurnIngestFingerprint(envelope)
	if canonicalErr != nil {
		if err == nil || transport != "" {
			t.Fatal("invalid canonical input was hashed")
		}

		return
	}

	if err != nil || transport != "boyan-ingest-fingerprint:turn:v1:"+canonical {
		t.Fatalf("transport bypassed canonical authority: %s %v", transport, err)
	}

	envelope.ProducerID = "another-producer"
	envelope.ScopeSequence = 9

	same, err := m.TurnIngestFingerprint(envelope)
	if err != nil || same != transport {
		t.Fatal("delivery locator changed payload identity")
	}
}

func TestTurnDeliveryFingerprintValidatesWholeEnvelope(t *testing.T) {
	for _, kind := range []string{"schema", "producer", "sequence", "payload"} {
		t.Run(kind, func(t *testing.T) {
			envelope := m.IngestEnvelope[m.Turn]{SchemaVersion: m.IngestHTTPSchemaVersionV1,
				ProducerID: "producer", ScopeSequence: 1, Payload: identityTestTurn(t)}

			switch kind {
			case "schema":
				envelope.SchemaVersion = "unsupported"
			case "producer":
				envelope.ProducerID = ""
			case "sequence":
				envelope.ScopeSequence = 0
			case "payload":
				envelope.Payload.Messages = nil
			}

			fingerprint, err := m.TurnIngestFingerprint(envelope)
			if err == nil || fingerprint != "" {
				t.Fatal("malformed envelope was hashed")
			}
		})
	}
}
