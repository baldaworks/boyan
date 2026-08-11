package memory

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMemoryRecordsRoundTripJSONAndRejectDuplicateEvidence(t *testing.T) {
	t.Parallel()

	revision := MemoryRevision{
		SchemaVersion: MemorySchemaVersion,
		RevisionID:    "revision-1",
		ItemID:        "item-1",
		Revision:      1,
		Temporal:      Temporal{ObservedAt: time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC)},
		Evidence: []EvidenceRef{{
			SourceID: "source-1", MessageID: "message-1", Role: MessageRoleUser,
			StartByte: 1, EndByte: 4, AssertionMode: AssertionModeUser,
		}},
		Sensitivity: SensitivityStandard,
		Retention:   RetentionClassStandard,
		Payload:     PayloadRef{ID: "payload-1", Digest: "digest-1", ByteSize: 7},
	}
	encoded, err := json.Marshal(revision)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var decoded MemoryRevision
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

	evidence := EvidenceRef{
		SourceID: "source-1", MessageID: "message-1", Role: MessageRoleTool,
		StartByte: 1, EndByte: 4, AssertionMode: AssertionModeTrustedTool,
	}
	if err := evidence.Validate(); err != nil {
		t.Fatalf("tool evidence.Validate() error = %v", err)
	}
	evidence.Role = MessageRoleAssistant
	if err := evidence.Validate(); err == nil {
		t.Fatal("assistant evidence was accepted as a trusted tool")
	}
}

func TestNewEvidenceRefUsesUTF8ByteOffsetsAndMessageDigest(t *testing.T) {
	text := "Привет world"
	evidence, err := NewEvidenceRef("source-1", "message-1", MessageRoleUser, text, 0, 12, AssertionModeUser)
	if err != nil {
		t.Fatalf("NewEvidenceRef() error = %v", err)
	}
	if evidence.TextDigest == "" || evidence.StartByte != 0 || evidence.EndByte != 12 {
		t.Fatalf("evidence = %+v", evidence)
	}
	if _, err := NewEvidenceRef("source-1", "message-1", MessageRoleUser, text, 1, 12, AssertionModeUser); err == nil {
		t.Fatal("NewEvidenceRef() accepted a byte offset inside a UTF-8 rune")
	}
	if _, err := NewEvidenceRef("source-1", "message-1", MessageRoleUser, text, 0, 99, AssertionModeUser); err == nil {
		t.Fatal("NewEvidenceRef() accepted an out-of-range byte span")
	}
}
