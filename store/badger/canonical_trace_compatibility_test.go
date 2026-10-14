package badger //nolint:testpackage // Exercises corrupt persisted records and compatibility payload decoding.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	m "github.com/baldaworks/boyan/pkg/memory"
	db "github.com/dgraph-io/badger/v4"
)

func TestCanonicalTraceStoredCompatibility(t *testing.T) {
	store, err := Open(t.TempDir())
	requireTraceNoError(t, err, "open: %v")
	t.Cleanup(func() { requireTraceNoError(t, store.Close(), "close: %v") })

	scope := m.Scope{Key: "test:compat", Kind: m.ScopeKindPersonal}
	turn := canonicalReaderTestTurn(t, scope, time.Now().UTC())
	itemID, revisionID, sourceBytes, content := storedTraceCompatibility(t, scope, turn)
	evidence,
		err := m.NewEvidenceRef(turn.SourceID,
		turn.Messages[0].MessageID,
		m.MessageRoleUser,
		turn.Messages[0].Text,
		0,
		uint32(len(turn.Messages[0].Text)),
		m.AssertionModeUser)
	requireTraceNoError(t, err, "evidence: %v")

	mutation := canonicalReaderTestMutation(scope, turn.CompletedAt, evidence, turn)
	mutation.Items[0].ItemID = itemID
	mutation.Revisions[0].ItemID = itemID
	mutation.Revisions[0].RevisionID = revisionID
	mutation.Revisions[0].Payload = payloadRef("compatibility", content)
	mutation.Sources[0].Payload = payloadRef("source", sourceBytes)
	mutation.Messages = nil
	mutation.Heads = []m.ItemHead{{ItemID: itemID, RevisionID: revisionID}}
	mutation.Payloads = []m.CanonicalPayload{{Ref: mutation.Revisions[0].Payload,
		Data: content},
		{Ref: mutation.Sources[0].Payload,
			Data: sourceBytes}}
	_, err = store.ApplyCanonicalMutation(context.Background(), mutation)
	requireTraceNoError(t, err, "commit: %v")
	makePrivacyHistoryOld(t, store, mutation)
	requireTraceNoError(t, store.PrepareSourcePrivacy(context.Background()), "prepare old embedded source: %v")

	if got := privacyState(t, store, scope, mutation.Sources[0].SourceID); got != sourcePrivacyStandard {
		t.Fatalf("legacy fixture is not authoritative standard: %s", got)
	}

	reader, err := NewCanonicalReader(store)
	requireTraceNoError(t, err, "reader: %v")
	response,
		err := reader.Trace(context.Background(),
		m.TraceRequest{Scope: scope,
			Root: m.RevisionRef{ItemID: itemID,
				RevisionID: revisionID},
			MaxNodes: 10})
	requireTraceNoError(t, err, "trace: %v")
	requireTraceNoError(t, response.Validate(10), "validate: %v")

	if len(response.Revisions) != 1 || response.Revisions[0].Atom == nil || response.Revisions[0].Canonical != nil {
		t.Fatalf("legacy shape: %+v",
			response)
	}
}

func TestTraceSourceDoesNotFallbackOnCorruption(t *testing.T) {
	store, err := Open(t.TempDir())
	requireTraceNoError(t, err, "open: %v")
	t.Cleanup(func() { requireTraceNoError(t, store.Close(), "close: %v") })

	scope := m.Scope{Key: "test:corrupt", Kind: m.ScopeKindPersonal}
	key, err := canonicalKey(scope, badgerRecordSource, "canonical-source")
	requireTraceNoError(t, err, "key: %v")
	err = store.db.Update(func(txn *db.Txn) error { return txn.Set(key, []byte("corrupt canonical envelope")) })
	requireTraceNoError(t, err, "corrupt: %v")
	reader, err := NewCanonicalReader(store)
	requireTraceNoError(t, err, "reader: %v")

	_,
		err = reader.traceSourceIdentity(scope,
		m.SourceRef{Scope: scope,
			ExportID:       "missing-legacy",
			ConversationID: "conversation",
			SourceTurnID:   "turn"},
		"canonical-source")
	if err == nil || strings.Contains(err.Error(), "Key not found") {
		t.Fatalf("corruption replaced by fallback: %v", err)
	}
}

func TestRecognizedMalformedMigratedTraceSource(t *testing.T) {
	scope := m.Scope{Key: "test:corrupt", Kind: m.ScopeKindPersonal}

	_,
		recognized,
		err := migratedTraceSourceRecord([]byte(`{"ref":{"scope":{"key":"test:corrupt",
"kind":"personal"}},
"export_id":"native",
"conversation_id":"conversation",
"source_turn_id":"turn"}`),
		scope)
	if !recognized || err == nil {
		t.Fatalf("recognized migrated payload fell through: %v %v", recognized, err)
	}
}

func requireTraceNoError(t *testing.T, err error, format string) {
	t.Helper()

	if err != nil {
		t.Fatalf(format, err)
	}
}

func storedTraceCompatibility(t *testing.T, scope m.Scope, turn m.Turn) (string, string, []byte, []byte) {
	t.Helper()

	ref := m.SourceRef{Scope: scope,
		ExportID:       turn.ExportID,
		ConversationID: turn.Conversation.ConversationID,
		SourceTurnID:   turn.SourceTurnID}
	provenance := m.Provenance{RawSources: []m.SourceRef{ref}}
	itemID, err := m.AtomItemID(scope, m.AtomCategoryFact, "legacy memory")
	requireTraceNoError(t, err, "item: %v")
	revisionID,
		err := m.DerivedRevisionID(scope,
		itemID,
		"legacy-operation",
		[]string{string(m.AtomCategoryFact),
			"legacy memory",
			string(m.CandidateRelationNew)},
		provenance,
		nil)
	requireTraceNoError(t, err, "revision: %v")

	category := m.AtomCategoryFact
	compatibility := m.CanonicalCompatibilityPayload{SchemaVersion: m.CanonicalCompatibilitySchemaVersion,
		Kind:              m.DerivedKindAtom,
		Category:          &category,
		Text:              "legacy memory",
		LegacyItemID:      itemID,
		LegacyRevisionID:  revisionID,
		LegacyOperationID: "legacy-operation"}
	source := m.SourceRecord{SchemaVersion: m.DerivedSchemaVersionV1, Ref: ref, State: m.SourceStateActive, Turn: &turn}
	sourceBytes, err := json.Marshal(source)
	requireTraceNoError(t, err, "source: %v")
	content, err := json.Marshal(compatibility)
	requireTraceNoError(t, err, "compatibility: %v")

	return itemID, revisionID, sourceBytes, content
}
