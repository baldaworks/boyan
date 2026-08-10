package badger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/baldaworks/boyan"
)

func TestCanonicalReaderHydratesRecallAndBoundedTrace(t *testing.T) {
	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "canonical.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	scope := boyan.Scope{Key: "canonical:reader", Kind: boyan.ScopeKindPersonal}
	completedAt := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	turn, err := boyan.NewTurn(scope, boyan.SessionRef{SessionID: "session-reader", AgentSessionID: "agent-reader"}, "turn-reader", completedAt, "remember canonical", "canonical result")
	if err != nil {
		t.Fatalf("NewTurn() error = %v", err)
	}
	sourcePayload, err := json.Marshal(canonicalTurnSourcePayload{ExportID: turn.ExportID, SessionID: turn.Session.SessionID, AgentSessionID: turn.Session.AgentSessionID, SourceTurnID: turn.SourceTurnID, CompletedAt: turn.CompletedAt, TerminalStatus: turn.TerminalStatus})
	if err != nil {
		t.Fatalf("marshal source payload: %v", err)
	}
	evidence, err := boyan.NewEvidenceRef(turn.SourceID, turn.Messages[0].MessageID, boyan.MessageRoleUser, turn.Messages[0].Text, 0, uint32(len(turn.Messages[0].Text)), boyan.AssertionModeUser)
	if err != nil {
		t.Fatalf("NewEvidenceRef() error = %v", err)
	}
	item := boyan.MemoryItem{ItemID: "item-reader", Scope: scope, Kind: boyan.MemoryKindState, MemoryKey: "reader-key"}
	revision := boyan.MemoryRevision{SchemaVersion: boyan.MemorySchemaVersionV2, RevisionID: "revision-reader", ItemID: item.ItemID, Revision: 1, Temporal: boyan.Temporal{ObservedAt: completedAt}, Evidence: []boyan.EvidenceRef{evidence}, Sensitivity: boyan.SensitivityStandard, Retention: boyan.RetentionClassStandard}
	mutation := boyan.CanonicalMutation{
		SchemaVersion: boyan.CanonicalSchemaVersionV1,
		Scope:         scope,
		Operation:     boyan.OperationRecord{OperationID: "operation-reader", Fingerprint: "fingerprint-reader", CommittedAt: completedAt},
		Sources:       []boyan.SourceRecordV2{{SourceID: turn.SourceID, Scope: scope, Sensitivity: boyan.SensitivityStandard, Retention: boyan.RetentionClassStandard}},
		Messages: []boyan.MessageRecord{
			{MessageID: turn.Messages[0].MessageID, SourceID: turn.SourceID, Role: boyan.MessageRoleUser},
			{MessageID: turn.Messages[1].MessageID, SourceID: turn.SourceID, Role: boyan.MessageRoleAssistant},
		},
		Items:     []boyan.MemoryItem{item},
		Revisions: []boyan.MemoryRevision{revision},
		Heads:     []boyan.ItemHead{{ItemID: item.ItemID, RevisionID: revision.RevisionID}},
	}
	mutation.Sources[0].Payload = payloadRef("source-reader", sourcePayload)
	mutation.Messages[0].Payload = payloadRef("message-reader", []byte(turn.Messages[0].Text))
	mutation.Messages[1].Payload = payloadRef("message-reader-assistant", []byte(turn.Messages[1].Text))
	revision.Payload = payloadRef("revision-reader", []byte("canonical result"))
	mutation.Revisions[0] = revision
	mutation.Payloads = []boyan.CanonicalPayload{{Ref: mutation.Sources[0].Payload, Data: sourcePayload}, {Ref: mutation.Messages[0].Payload, Data: []byte(turn.Messages[0].Text)}, {Ref: mutation.Messages[1].Payload, Data: []byte(turn.Messages[1].Text)}, {Ref: revision.Payload, Data: []byte("canonical result")}}
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatalf("ApplyCanonicalMutation() error = %v", err)
	}

	reader, err := NewCanonicalReader(store)
	if err != nil {
		t.Fatalf("NewCanonicalReader() error = %v", err)
	}
	records, err := reader.LoadRecallRecords(context.Background(), scope, []string{revision.RevisionID})
	if err != nil || len(records) != 1 {
		t.Fatalf("LoadRecallRecords() = %#v, error = %v", records, err)
	}
	if records[0].Text != "canonical result" || records[0].SessionIDs[0] != turn.Session.SessionID || records[0].SourceIDs[0] != turn.SourceID {
		t.Fatalf("hydrated record = %#v", records[0])
	}
	search, err := reader.SearchDerived(context.Background(), boyan.DerivedSearchRequest{Scope: scope, Query: "canonical", Limit: 10})
	if err != nil || len(search.Results) != 1 || search.Results[0].RevisionID != revision.RevisionID {
		t.Fatalf("SearchDerived() = %#v, error = %v", search, err)
	}
	if _, err := reader.Trace(context.Background(), boyan.TraceRequest{Scope: scope, Root: boyan.RevisionRef{ItemID: item.ItemID, RevisionID: revision.RevisionID}, MaxNodes: 10}); err == nil {
		t.Fatal("Trace() unexpectedly accepted a v2 item without a representable legacy identity")
	}
}

func payloadRef(id string, data []byte) boyan.PayloadRef {
	digest := sha256.Sum256(data)
	return boyan.PayloadRef{ID: id, Digest: hex.EncodeToString(digest[:]), ByteSize: uint32(len(data))}
}
