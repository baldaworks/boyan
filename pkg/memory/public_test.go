package memory_test

import (
	"testing"
	"time"

	"github.com/baldaworks/boyan/pkg/memory"
)

func TestNewTurnBuildsStableStandaloneExport(t *testing.T) {
	scope := memory.Scope{Key: "chat:scope-1", Kind: memory.ScopeKindPersonal}
	conversation := memory.ConversationRef{ConversationID: "conversation-1", AgentConversationID: "agent-conversation-1"}

	turn, err := memory.NewTurn(
		scope,
		conversation,
		"turn-1",
		time.Date(2026, time.August, 10, 9, 0, 0, 0, time.UTC),
		"hello",
		"hi",
	)
	if err != nil {
		t.Fatalf("NewTurn() error = %v", err)
	}

	if err := turn.Validate(); err != nil {
		t.Fatalf("turn.Validate() error = %v", err)
	}

	if turn.Scope != scope {
		t.Fatalf("turn.Scope = %+v, want %+v", turn.Scope, scope)
	}

	if turn.SourceID == "" {
		t.Fatal("turn.SourceID is empty")
	}
}

func TestIngestReceiptFromOutcome(t *testing.T) {
	outcome := memory.IngestOutcome{
		SchemaVersion:  memory.IngestOutcomeSchemaVersionV1,
		Kind:           memory.IngestKindTurn,
		Disposition:    memory.IngestDispositionAccepted,
		Projection:     memory.ProjectionStatusCurrent,
		ProducerID:     "producer-1",
		Scope:          memory.Scope{Key: "chat:scope-1", Kind: memory.ScopeKindPersonal},
		ScopeSequence:  1,
		Fingerprint:    "fingerprint-1",
		ExportID:       "export-1",
		OperationIDs:   []string{"operation-1"},
		ScopeVersion:   1,
		ScopeChangeSeq: 1,
	}

	receipt, err := memory.IngestReceiptFromOutcome(outcome)
	if err != nil {
		t.Fatalf("IngestReceiptFromOutcome() error = %v", err)
	}

	if err := receipt.Validate(); err != nil {
		t.Fatalf("receipt.Validate() error = %v", err)
	}

	if receipt.OperationID != "operation-1" {
		t.Fatalf("receipt.OperationID = %q, want %q", receipt.OperationID, "operation-1")
	}
}
