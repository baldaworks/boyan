package orderedingest

import (
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func testSequencedTurn(t *testing.T, sequence uint64, fingerprint string) boyan.SequencedTurn {
	return testSequencedTurnWithID(t, sequence, fingerprint, "turn-1")
}

func testSequencedTurnWithID(t *testing.T, sequence uint64, fingerprint, turnID string) boyan.SequencedTurn {
	t.Helper()
	turn, err := boyan.NewTurn(
		boyan.Scope{Key: "telegram:sequenced-ingest", Kind: boyan.ScopeKindPersonal},
		boyan.ConversationRef{ConversationID: "conversation-1", AgentConversationID: "agent-1"},
		turnID,
		time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC),
		"Ada",
		"Ada lives in Bishkek",
	)
	if err != nil {
		t.Fatalf("NewTurn() error = %v", err)
	}
	return boyan.SequencedTurn{
		ProducerID:    "producer-1",
		ScopeSequence: sequence,
		Fingerprint:   fingerprint,
		Turn:          turn,
	}
}

func testSequencedBoundary(t *testing.T, sequence uint64, fingerprint string) boyan.SequencedBoundary {
	t.Helper()
	boundary, err := boyan.NewBoundary(
		boyan.Scope{Key: "telegram:sequenced-boundary", Kind: boyan.ScopeKindPersonal},
		boyan.ConversationRef{ConversationID: "conversation-boundary", AgentConversationID: "agent-boundary"},
		"transition-1",
		boyan.BoundaryReasonClose,
		time.Date(2026, time.August, 9, 12, 5, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("NewBoundary() error = %v", err)
	}
	return boyan.SequencedBoundary{
		ProducerID:    "producer-1",
		ScopeSequence: sequence,
		Fingerprint:   fingerprint,
		Boundary:      boundary,
	}
}
