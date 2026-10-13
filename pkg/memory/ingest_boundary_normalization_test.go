package memory_test

import (
	"testing"
	"time"

	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestBoundaryDeliveryFingerprintNormalizesEquivalentInputs(t *testing.T) {
	boundary, err := m.NewBoundary(m.Scope{Key: "delivery:normalization", Kind: m.ScopeKindPersonal},
		m.ConversationRef{ConversationID: "c", AgentConversationID: "a"}, "close", m.BoundaryReasonClose,
		time.Date(2026, time.October, 14, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	envelope := m.IngestEnvelope[m.Boundary]{SchemaVersion: m.IngestHTTPSchemaVersionV1,
		ProducerID: "producer", ScopeSequence: 1, Payload: boundary}

	first, err := m.BoundaryIngestFingerprint(envelope)
	if err != nil {
		t.Fatal(err)
	}

	envelope.Payload.TransitionID = " close "
	envelope.Payload.OccurredAt = boundary.OccurredAt.In(time.FixedZone("offset", 3600))

	second, err := m.BoundaryIngestFingerprint(envelope)
	if err != nil || second != first {
		t.Fatalf("equivalent boundary fingerprint changed: %s %s %v", first, second, err)
	}
}
