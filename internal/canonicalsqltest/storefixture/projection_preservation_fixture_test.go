package storefixture_test

import (
	"context"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Original fixture DTOs are retained; only authority construction changes.
type projectionOwner interface {
	memory.CanonicalStore
	memory.ProjectionCheckpointStore
	memory.CanonicalOperationReader
	storefixture.Store
	PrepareSourcePrivacy(ctx context.Context) error
	SetProjectionRetentionFloor(ctx context.Context, scope memory.Scope, projection string, floor uint64) error
	IsSourceDenied(ctx context.Context, scope memory.Scope, id string) (bool, error)
	IsRevisionDenied(ctx context.Context, scope memory.Scope, id string) (bool, error)
}

func requireProjectionNoError(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
}

func forEachCoordinatorAuthority(t *testing.T, run func(*testing.T, canonicalsqltest.Backend)) {
	t.Helper()

	for _, backend := range []canonicalsqltest.Backend{canonicalsqltest.SQLite, canonicalsqltest.Postgres} {
		t.Run(string(backend), func(t *testing.T) { run(t, backend) })
	}
}

func canonicalReaderTestTurn(t *testing.T, scope memory.Scope, completedAt time.Time) memory.Turn {
	t.Helper()

	turn, err := memory.NewTurn(
		scope,
		memory.ConversationRef{ConversationID: "conversation-reader", AgentConversationID: "agent-reader"},
		"turn-reader",
		completedAt,
		"remember canonical",
		"canonical result",
	)
	if err != nil {
		t.Fatalf("NewTurn() error = %v", err)
	}

	return turn
}

func canonicalReaderTestMutation(
	scope memory.Scope,
	completedAt time.Time,
	evidence memory.EvidenceRef,
	turn memory.Turn,
) memory.CanonicalMutation {
	item := memory.MemoryItem{
		ItemID:    "item-reader",
		Scope:     scope,
		Kind:      memory.MemoryKindState,
		MemoryKey: "reader-key",
	}
	revision := memory.MemoryRevision{
		SchemaVersion: memory.MemorySchemaVersion,
		RevisionID:    "revision-reader",
		ItemID:        item.ItemID,
		Revision:      1,
		Temporal:      memory.Temporal{ObservedAt: completedAt},
		Evidence:      []memory.EvidenceRef{evidence},
		Sensitivity:   memory.SensitivityStandard,
		Retention:     memory.RetentionClassStandard,
	}
	mutation := memory.CanonicalMutation{
		SchemaVersion: memory.CanonicalSchemaVersionV1,
		Scope:         scope,
		Operation: memory.OperationRecord{
			OperationID: "operation-reader",
			Fingerprint: "fingerprint-reader",
			CommittedAt: completedAt,
		},
		Sources:   canonicalReaderTestSources(turn),
		Messages:  canonicalReaderTestMessages(turn),
		Items:     []memory.MemoryItem{item},
		Revisions: []memory.MemoryRevision{revision},
		Heads:     []memory.ItemHead{{ItemID: item.ItemID, RevisionID: revision.RevisionID}},
	}

	return mutation
}

func canonicalReaderTestSources(turn memory.Turn) []memory.CanonicalSourceRecord {
	return []memory.CanonicalSourceRecord{
		{
			SourceID:    turn.SourceID,
			Scope:       turn.Scope,
			Sensitivity: memory.SensitivityStandard,
			Retention:   memory.RetentionClassStandard,
		},
	}
}

func canonicalReaderTestMessages(turn memory.Turn) []memory.MessageRecord {
	return []memory.MessageRecord{
		{
			MessageID: turn.Messages[0].MessageID,
			SourceID:  turn.SourceID,
			Role:      memory.MessageRoleUser,
		},
		{
			MessageID: turn.Messages[1].MessageID,
			SourceID:  turn.SourceID,
			Role:      memory.MessageRoleAssistant,
		},
	}
}

func canonicalRevisionMutation(
	scope memory.Scope,
	expectedVersion uint64,
	operationID string,
	revision memory.MemoryRevision,
) memory.CanonicalMutation {
	return memory.CanonicalMutation{
		SchemaVersion:        memory.CanonicalSchemaVersionV1,
		Scope:                scope,
		ExpectedScopeVersion: expectedVersion,
		Operation: memory.OperationRecord{
			OperationID: operationID,
			Fingerprint: operationID + "-fingerprint",
			CommittedAt: time.Date(2026, time.August, 5, 12, 0, int(expectedVersion), 0, time.UTC),
		},
		Items:     []memory.MemoryItem{canonicalItem(scope, revision.ItemID)},
		Revisions: []memory.MemoryRevision{revision},
		Heads:     []memory.ItemHead{{ItemID: revision.ItemID, RevisionID: revision.RevisionID}},
	}
}

func canonicalItem(scope memory.Scope, itemID string) memory.MemoryItem {
	return memory.MemoryItem{ItemID: itemID, Scope: scope, Kind: memory.MemoryKindEvent}
}
