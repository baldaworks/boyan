package memory_test

import memory "github.com/baldaworks/boyan/pkg/memory"

import (
	"bytes"
	"testing"
	"time"
)

func TestMemoryCandidateAndEnvelopeAreDeterministic(t *testing.T) {
	t.Parallel()

	candidate := memory.MemoryCandidate{
		Kind:      memory.MemoryKindState,
		Statement: "Uses Go",
		Temporal:  memory.Temporal{ObservedAt: time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC)},
		Evidence: []memory.EvidenceRef{{
			SourceID: "source-1", MessageID: "message-1", Role: memory.MessageRoleUser,
			StartByte: 1, EndByte: 4, AssertionMode: memory.AssertionModeUser,
		}},
		Sensitivity: memory.SensitivityStandard,
		Retention:   memory.RetentionClassStandard,
	}
	if err := candidate.Validate(); err != nil {
		t.Fatalf("candidate.Validate() error = %v", err)
	}

	first, err := memory.MarshalRecordEnvelope("memory_revision", []byte("payload"))
	if err != nil {
		t.Fatalf("MarshalRecordEnvelope() error = %v", err)
	}

	second, err := memory.MarshalRecordEnvelope("memory_revision", []byte("payload"))
	if err != nil {
		t.Fatalf("MarshalRecordEnvelope() second error = %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Fatalf("envelope is not deterministic: %x != %x", first, second)
	}

	envelope, err := memory.UnmarshalRecordEnvelope(first)
	if err != nil {
		t.Fatalf("UnmarshalRecordEnvelope() error = %v", err)
	}

	if envelope.RecordType != "memory_revision" ||
		!bytes.Equal(envelope.Payload, []byte("payload")) {
		t.Fatalf("decoded envelope = %+v", envelope)
	}

	first[len(first)-1] ^= 1
	if _, err := memory.UnmarshalRecordEnvelope(first); err == nil {
		t.Fatal("tampered envelope decoded successfully")
	}
}
