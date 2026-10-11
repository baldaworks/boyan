package memory_test

import memory "github.com/baldaworks/boyan/pkg/memory"

import (
	"testing"
	"time"
)

func TestCanonicalMutationValidation(t *testing.T) {
	scope := memory.Scope{Key: "canonical:scope", Kind: memory.ScopeKindPersonal}

	mutation := memory.CanonicalMutation{
		SchemaVersion: memory.CanonicalSchemaVersionV1,
		Scope:         scope,
		Operation: memory.OperationRecord{
			OperationID: "operation-1",
			Fingerprint: "derivation-current-sha256",
			CommittedAt: time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC),
		},
		Heads: []memory.ItemHead{{ItemID: "item-1", RevisionID: "revision-1"}},
	}
	if err := mutation.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	mutation.Operation.Fingerprint = " fingerprint"
	if err := mutation.Validate(); err == nil {
		t.Fatal("Validate() accepted a non-canonical operation fingerprint")
	}
}

func TestCanonicalMutationValidationRejectsForeignScope(t *testing.T) {
	mutation := memory.CanonicalMutation{
		SchemaVersion: memory.CanonicalSchemaVersionV1,
		Scope:         memory.Scope{Key: "canonical:scope", Kind: memory.ScopeKindPersonal},
		Operation: memory.OperationRecord{
			OperationID: "operation-1",
			Fingerprint: "derivation-current-sha256",
			CommittedAt: time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC),
		},
		Items: []memory.MemoryItem{{
			ItemID:    "item-1",
			Scope:     memory.Scope{Key: "foreign:scope", Kind: memory.ScopeKindPersonal},
			Kind:      memory.MemoryKindState,
			MemoryKey: "memory-key-1",
		}},
	}
	if err := mutation.Validate(); err == nil {
		t.Fatal("Validate() accepted a foreign item scope")
	}
}

func TestCanonicalMutationValidationRejectsDuplicateHead(t *testing.T) {
	mutation := memory.CanonicalMutation{
		SchemaVersion: memory.CanonicalSchemaVersionV1,
		Scope:         memory.Scope{Key: "canonical:scope", Kind: memory.ScopeKindPersonal},
		Operation: memory.OperationRecord{
			OperationID: "operation-1",
			Fingerprint: "derivation-current-sha256",
			CommittedAt: time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC),
		},
		Heads: []memory.ItemHead{
			{ItemID: "item-1", RevisionID: "revision-1"},
			{ItemID: "item-1", RevisionID: "revision-2"},
		},
	}
	if err := mutation.Validate(); err == nil {
		t.Fatal("Validate() accepted duplicate item heads")
	}
}

func TestCanonicalMutationOutcomeValidation(t *testing.T) {
	outcome := memory.CanonicalMutationOutcome{ScopeVersion: 1, ChangeSeq: 1, RevisionIDs: []string{"revision-1"}}
	if err := outcome.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
