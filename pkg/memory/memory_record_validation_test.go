package memory_test

import memory "github.com/baldaworks/boyan/pkg/memory"

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMemoryRecordsRoundTripJSONAndRejectDuplicateEvidence(t *testing.T) {
	t.Parallel()

	revision := memory.MemoryRevision{
		SchemaVersion: memory.MemorySchemaVersion,
		RevisionID:    "revision-1",
		ItemID:        "item-1",
		Revision:      1,
		Temporal:      memory.Temporal{ObservedAt: time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC)},
		Evidence: []memory.EvidenceRef{{
			SourceID: "source-1", MessageID: "message-1", Role: memory.MessageRoleUser,
			StartByte: 1, EndByte: 4, AssertionMode: memory.AssertionModeUser,
		}},
		Sensitivity: memory.SensitivityStandard,
		Retention:   memory.RetentionClassStandard,
		Payload:     memory.PayloadRef{ID: "payload-1", Digest: "digest-1", ByteSize: 7},
	}

	encoded, err := json.Marshal(revision)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var decoded memory.MemoryRevision
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if err := decoded.Validate(); err != nil {
		t.Fatalf("decoded.Validate() error = %v", err)
	}

	decoded.Evidence = append(decoded.Evidence, decoded.Evidence[0])
	if err := decoded.Validate(); err == nil {
		t.Fatal("duplicate evidence validated")
	}

	decoded.Evidence = decoded.Evidence[:1]

	decoded.Parents = []string{decoded.RevisionID}
	if err := decoded.Validate(); err == nil {
		t.Fatal("self-parenting revision validated")
	}

	decoded.Parents = []string{"parent-1", "parent-1"}
	if err := decoded.Validate(); err == nil {
		t.Fatal("duplicate revision parents validated")
	}
}

func TestTrustedToolEvidenceRequiresToolRole(t *testing.T) {
	t.Parallel()

	evidence := memory.EvidenceRef{
		SourceID: "source-1", MessageID: "message-1", Role: memory.MessageRoleTool,
		StartByte: 1, EndByte: 4, AssertionMode: memory.AssertionModeTrustedTool,
	}
	if err := evidence.Validate(); err != nil {
		t.Fatalf("tool evidence.Validate() error = %v", err)
	}

	evidence.Role = memory.MessageRoleAssistant
	if err := evidence.Validate(); err == nil {
		t.Fatal("assistant evidence was accepted as a trusted tool")
	}
}

func TestNewEvidenceRefUsesUTF8ByteOffsetsAndMessageDigest(t *testing.T) {
	text := "Привет world"

	evidence, err := memory.NewEvidenceRef(
		"source-1",
		"message-1",
		memory.MessageRoleUser,
		text,
		0,
		12,
		memory.AssertionModeUser,
	)
	if err != nil {
		t.Fatalf("NewEvidenceRef() error = %v", err)
	}

	if evidence.TextDigest == "" || evidence.StartByte != 0 || evidence.EndByte != 12 {
		t.Fatalf("evidence = %+v", evidence)
	}

	if _, err := memory.NewEvidenceRef(
		"source-1",
		"message-1",
		memory.MessageRoleUser,
		text,
		1,
		12,
		memory.AssertionModeUser,
	); err == nil {
		t.Fatal("NewEvidenceRef() accepted a byte offset inside a UTF-8 rune")
	}

	if _, err := memory.NewEvidenceRef(
		"source-1",
		"message-1",
		memory.MessageRoleUser,
		text,
		0,
		99,
		memory.AssertionModeUser,
	); err == nil {
		t.Fatal("NewEvidenceRef() accepted an out-of-range byte span")
	}
}
