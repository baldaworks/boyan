package badger //nolint:testpackage // Constructs the private canonical turn payload schema to verify reader hydration.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func TestCanonicalReaderHydratesRecallAndBoundedTrace(t *testing.T) {
	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "canonical.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}

	t.Cleanup(func() { _ = store.Close() })

	scope := boyan.Scope{Key: "canonical:reader", Kind: boyan.ScopeKindPersonal}
	completedAt := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)

	turn := canonicalReaderTestTurn(t, scope, completedAt)

	sourcePayload := canonicalReaderTestSourcePayload(t, turn)

	evidence, err := boyan.NewEvidenceRef(
		turn.SourceID,
		turn.Messages[0].MessageID,
		boyan.MessageRoleUser,
		turn.Messages[0].Text,
		0,
		uint32(len(turn.Messages[0].Text)),
		boyan.AssertionModeUser,
	)
	if err != nil {
		t.Fatalf("NewEvidenceRef() error = %v", err)
	}

	mutation := canonicalReaderTestMutation(scope, completedAt, evidence, turn)
	item := mutation.Items[0]
	revision := mutation.Revisions[0]

	mutation.Sources[0].Payload = payloadRef("source-reader", sourcePayload)
	mutation.Messages[0].Payload = payloadRef("message-reader", []byte(turn.Messages[0].Text))
	mutation.Messages[1].Payload = payloadRef("message-reader-assistant", []byte(turn.Messages[1].Text))
	revision.Payload = payloadRef("revision-reader", []byte("canonical result"))
	mutation.Revisions[0] = revision

	mutation.Payloads = []boyan.CanonicalPayload{
		{Ref: mutation.Sources[0].Payload, Data: sourcePayload},
		{Ref: mutation.Messages[0].Payload, Data: []byte(turn.Messages[0].Text)},
		{Ref: mutation.Messages[1].Payload, Data: []byte(turn.Messages[1].Text)},
		{Ref: revision.Payload, Data: []byte("canonical result")},
	}
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatalf("ApplyCanonicalMutation() error = %v", err)
	}

	reader, err := NewCanonicalReader(store)
	if err != nil {
		t.Fatalf("NewCanonicalReader() error = %v", err)
	}

	assertCanonicalReaderHydration(t, reader, scope, turn, item, revision)
}

func payloadRef(id string, data []byte) boyan.PayloadRef {
	digest := sha256.Sum256(data)

	return boyan.PayloadRef{ID: id, Digest: hex.EncodeToString(digest[:]), ByteSize: uint32(len(data))}
}

func assertCanonicalReaderHydration(
	t *testing.T,
	reader *CanonicalReader,
	scope boyan.Scope,
	turn boyan.Turn,
	item boyan.MemoryItem,
	revision boyan.MemoryRevision,
) {
	t.Helper()

	records, err := reader.LoadRecallRecords(context.Background(), scope, []string{revision.RevisionID})
	if err != nil || len(records) != 1 {
		t.Fatalf("LoadRecallRecords() = %#v, error = %v", records, err)
	}

	if records[0].Text != "canonical result" ||
		records[0].ConversationIDs[0] != turn.Conversation.ConversationID ||
		records[0].SourceIDs[0] != turn.SourceID {
		t.Fatalf("hydrated record = %#v", records[0])
	}

	search, err := reader.SearchDerived(
		context.Background(),
		boyan.DerivedSearchRequest{Scope: scope, Query: "canonical", Limit: 10},
	)
	if err != nil || len(search.Results) != 1 ||
		search.Results[0].RevisionID != revision.RevisionID {
		t.Fatalf("SearchDerived() = %#v, error = %v", search, err)
	}

	assertNativeReaderTrace(t, reader, scope, item, revision)
}

func canonicalReaderTestTurn(t *testing.T, scope boyan.Scope, completedAt time.Time) boyan.Turn {
	t.Helper()

	turn, err := boyan.NewTurn(
		scope,
		boyan.ConversationRef{ConversationID: "conversation-reader", AgentConversationID: "agent-reader"},
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
	scope boyan.Scope,
	completedAt time.Time,
	evidence boyan.EvidenceRef,
	turn boyan.Turn,
) boyan.CanonicalMutation {
	item := boyan.MemoryItem{
		ItemID:    "item-reader",
		Scope:     scope,
		Kind:      boyan.MemoryKindState,
		MemoryKey: "reader-key",
	}
	revision := boyan.MemoryRevision{
		SchemaVersion: boyan.MemorySchemaVersion,
		RevisionID:    "revision-reader",
		ItemID:        item.ItemID,
		Revision:      1,
		Temporal:      boyan.Temporal{ObservedAt: completedAt},
		Evidence:      []boyan.EvidenceRef{evidence},
		Sensitivity:   boyan.SensitivityStandard,
		Retention:     boyan.RetentionClassStandard,
	}
	mutation := boyan.CanonicalMutation{
		SchemaVersion: boyan.CanonicalSchemaVersionV1,
		Scope:         scope,
		Operation: boyan.OperationRecord{
			OperationID: "operation-reader",
			Fingerprint: "fingerprint-reader",
			CommittedAt: completedAt,
		},
		Sources:   canonicalReaderTestSources(turn),
		Messages:  canonicalReaderTestMessages(turn),
		Items:     []boyan.MemoryItem{item},
		Revisions: []boyan.MemoryRevision{revision},
		Heads:     []boyan.ItemHead{{ItemID: item.ItemID, RevisionID: revision.RevisionID}},
	}

	return mutation
}

func canonicalReaderTestSources(turn boyan.Turn) []boyan.CanonicalSourceRecord {
	return []boyan.CanonicalSourceRecord{
		{
			SourceID:    turn.SourceID,
			Scope:       turn.Scope,
			Sensitivity: boyan.SensitivityStandard,
			Retention:   boyan.RetentionClassStandard,
		},
	}
}

func canonicalReaderTestMessages(turn boyan.Turn) []boyan.MessageRecord {
	return []boyan.MessageRecord{
		{
			MessageID: turn.Messages[0].MessageID,
			SourceID:  turn.SourceID,
			Role:      boyan.MessageRoleUser,
		},
		{
			MessageID: turn.Messages[1].MessageID,
			SourceID:  turn.SourceID,
			Role:      boyan.MessageRoleAssistant,
		},
	}
}

func canonicalReaderTestSourcePayload(t *testing.T, turn boyan.Turn) []byte {
	t.Helper()

	sourcePayload, err := json.Marshal(
		canonicalTurnSourcePayload{
			ExportID:            turn.ExportID,
			ConversationID:      turn.Conversation.ConversationID,
			AgentConversationID: turn.Conversation.AgentConversationID,
			SourceTurnID:        turn.SourceTurnID,
			CompletedAt:         turn.CompletedAt,
			TerminalStatus:      turn.TerminalStatus,
		},
	)
	if err != nil {
		t.Fatalf("marshal source payload: %v", err)
	}

	return sourcePayload
}

func assertNativeReaderTrace(t *testing.T,
	reader *CanonicalReader,
	scope boyan.Scope,
	item boyan.MemoryItem,
	revision boyan.MemoryRevision) {
	t.Helper()

	response, err := reader.Trace(context.Background(), boyan.TraceRequest{
		Scope: scope, Root: boyan.RevisionRef{ItemID: item.ItemID, RevisionID: revision.RevisionID}, MaxNodes: 10,
	})
	if err != nil {
		t.Fatalf("Trace() error = %v", err)
	}

	if len(response.Revisions) != 1 || response.Revisions[0].Canonical == nil {
		t.Fatalf("native trace = %+v", response)
	}

	if err := response.Validate(10); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
