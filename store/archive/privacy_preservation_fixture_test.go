package archive_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/postgres"
	"github.com/baldaworks/boyan/store/sqlite"
)

type privacyPreservationStore interface {
	logicalStore
	ApplyCanonicalMutation(ctx context.Context, mutation memory.CanonicalMutation) (memory.CanonicalMutationOutcome, error)
	LoadScopeState(ctx context.Context, scope memory.Scope) (memory.ScopeState, error)
}

type privacyPreservationReader interface {
	Trace(ctx context.Context, request memory.TraceRequest) (memory.TraceResponse, error)
	LoadRecallRecords(ctx context.Context, scope memory.Scope, ids []string) ([]memory.RecallRecord, error)
}

func privacyPreservationRef(id string, data []byte) memory.PayloadRef {
	digest := sha256.Sum256(data)

	return memory.PayloadRef{ID: id, Digest: hex.EncodeToString(digest[:]), ByteSize: uint32(len(data))}
}

// The two raw messages and independent revision payload make classification
// and recall distinct: restricting source disclosure must preserve benign memory.
func privacyPreservationMutation(t *testing.T, scope memory.Scope, id string) (memory.CanonicalMutation, memory.Turn) {
	t.Helper()

	turn, err := memory.NewTurn(scope, memory.ConversationRef{ConversationID: "conversation",
		AgentConversationID: "agent"},
		id, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "remember canonical", "canonical result")
	if err != nil {
		t.Fatal(err)
	}

	header, err := json.Marshal(map[string]any{"export_id": turn.ExportID,
		"conversation_id":       turn.Conversation.ConversationID,
		"agent_conversation_id": turn.Conversation.AgentConversationID, "source_turn_id": turn.SourceTurnID,
		"completed_at": turn.CompletedAt, "terminal_status": turn.TerminalStatus})
	if err != nil {
		t.Fatal(err)
	}

	source := memory.CanonicalSourceRecord{SourceID: turn.SourceID, Scope: scope,
		Payload: privacyPreservationRef(id+"-source", header), Sensitivity: memory.SensitivityStandard,
		Retention: memory.RetentionClassStandard}

	mutation := memory.CanonicalMutation{SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: scope,
		Operation: memory.OperationRecord{OperationID: id, Fingerprint: id, CommittedAt: turn.CompletedAt},
		Sources:   []memory.CanonicalSourceRecord{source},
		Payloads:  []memory.CanonicalPayload{{Ref: source.Payload, Data: header}}}
	for _, message := range turn.Messages {
		ref := privacyPreservationRef(message.MessageID, []byte(message.Text))
		mutation.Messages = append(mutation.Messages, memory.MessageRecord{MessageID: message.MessageID,
			SourceID: turn.SourceID, Role: message.Role, Payload: ref})
		mutation.Payloads = append(mutation.Payloads, memory.CanonicalPayload{Ref: ref, Data: []byte(message.Text)})
	}

	evidence, err := memory.NewEvidenceRef(turn.SourceID, turn.Messages[0].MessageID, memory.MessageRoleUser,
		turn.Messages[0].Text, 0, uint32(len(turn.Messages[0].Text)), memory.AssertionModeUser)
	if err != nil {
		t.Fatal(err)
	}

	item := memory.MemoryItem{ItemID: id + "-item", Scope: scope, Kind: memory.MemoryKindState,
		MemoryKey: memory.MemoryKey(id)}
	revision := memory.MemoryRevision{SchemaVersion: memory.MemorySchemaVersion,
		RevisionID: id + "-revision", ItemID: item.ItemID,
		Revision: 1, Temporal: memory.Temporal{ObservedAt: turn.CompletedAt}, Evidence: []memory.EvidenceRef{evidence},
		Sensitivity: memory.SensitivityStandard, Retention: memory.RetentionClassStandard,
		Payload: privacyPreservationRef(id+"-memory", []byte("protected memory"))}
	mutation.Items = []memory.MemoryItem{item}
	mutation.Revisions = []memory.MemoryRevision{revision}
	mutation.Heads = []memory.ItemHead{{ItemID: item.ItemID, RevisionID: revision.RevisionID}}

	mutation.Payloads = append(mutation.Payloads, memory.CanonicalPayload{Ref: revision.Payload,
		Data: []byte("protected memory")})
	for _, err := range []error{source.Validate(), item.Validate(), revision.Validate()} {
		if err != nil {
			t.Fatal(err)
		}
	}

	return mutation, turn
}

func privacyPreservationSeed(t *testing.T, f *storefixture.Fixture) (memory.CanonicalMutation, memory.Turn) {
	t.Helper()

	s := f.Store.(privacyPreservationStore)
	if err := s.PrepareSourcePrivacy(t.Context()); err != nil {
		t.Fatal(err)
	}

	m, turn := privacyPreservationMutation(t, memory.Scope{Key: "test:archive-privacy",
		Kind: memory.ScopeKindPersonal}, "first")
	if _, err := s.ApplyCanonicalMutation(t.Context(), m); err != nil {
		t.Fatal(err)
	}
	// Ordinary mutations need not write a complete scope coverage row. This
	// valid control makes scope-flag corruption target persisted metadata.
	privacyPreservationWrite(t, newArchiveProbe(t, f),
		privacyPreservationKey(canonicalsql.SourcePrivacyScope, m, "coverage"),
		map[string]any{"version": 1, "scope": m.Scope, "incomplete": false})
	privacyPreservationTrace(t, f, m, "")

	if err := s.VerifyCanonicalIntegrity(t.Context()); err != nil {
		t.Fatal("invalid positive source: ", err)
	}

	return m, turn
}

func privacyPreservationPublicReader(t *testing.T, f *storefixture.Fixture) privacyPreservationReader {
	t.Helper()

	switch s := f.Store.(type) {
	case *sqlite.Store:
		return sqlite.NewCanonicalReader(s)
	case *postgres.Store:
		return postgres.NewCanonicalReader(s)
	default:
		t.Fatal("unknown SQL owner")

		return nil
	}
}

func privacyPreservationTrace(t *testing.T, f *storefixture.Fixture, m memory.CanonicalMutation,
	want memory.ErrorCode) {
	t.Helper()

	got, err := privacyPreservationPublicReader(t, f).Trace(t.Context(), memory.TraceRequest{Scope: m.Scope,
		Root: memory.RevisionRef{ItemID: m.Items[0].ItemID, RevisionID: m.Revisions[0].RevisionID}, MaxNodes: 10})
	if want == "" {
		if err != nil || len(got.Sources) != 1 || len(got.Revisions) != 1 {
			t.Fatalf("standard graph=%+v error=%v", got, err)
		}

		return
	}

	code, _, ok := memory.ClassifyError(err)
	if !ok || code != want || len(got.Sources) != 0 || len(got.Revisions) != 0 {
		t.Fatalf("trace=%+v error=%v want=%s", got, err, want)
	}
}

func privacyPreservationUnavailable(t *testing.T, f *storefixture.Fixture, m memory.CanonicalMutation) {
	t.Helper()
	privacyPreservationTrace(t, f, m, memory.CodeUnavailable)
	rows, err := privacyPreservationPublicReader(t, f).LoadRecallRecords(t.Context(), m.Scope,
		[]string{m.Revisions[0].RevisionID})

	code, _, ok := memory.ClassifyError(err)
	if !ok || code != memory.CodeUnavailable || len(rows) != 0 {
		t.Fatalf("fenced recall=%+v error=%v", rows, err)
	}
}

func privacyPreservationReopen(t *testing.T, f *storefixture.Fixture) {
	t.Helper()

	if err := f.Store.Close(); err != nil {
		t.Fatal(err)
	}

	var err error

	f.Store, err = f.Open(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := f.Store.Close(); err != nil {
			t.Error(err)
		}
	})
}

func privacyPreservationKey(family canonicalsql.Family, m memory.CanonicalMutation, id string) canonicalsql.Key {
	return canonicalsql.Key{Family: family, Scope: m.Scope, Parts: [][]byte{[]byte(id)}}
}

func privacyPreservationRead(t *testing.T, p archiveProbe, key canonicalsql.Key) map[string]any {
	t.Helper()

	var value map[string]any

	if err := p.c.View(t.Context(), func(tx *canonicalsql.Tx) error {
		data, err := tx.Get(t.Context(), key)
		if err != nil {
			return err
		}

		envelope, err := memory.UnmarshalRecordEnvelope(data)
		if err != nil {
			return err
		}

		return json.Unmarshal(envelope.Payload, &value)
	}); err != nil {
		t.Fatal(err)
	}

	return value
}

func privacyPreservationWrite(t *testing.T, p archiveProbe, key canonicalsql.Key, value any) {
	t.Helper()

	data, err := canonicalsql.Encode(key.Family, value)
	if err != nil {
		t.Fatal(err)
	}

	if err := p.c.Update(t.Context(), func(tx *canonicalsql.Tx) error {
		return tx.Put(t.Context(), key,
			data)
	}); err != nil {
		t.Fatal(err)
	}
}

func privacyPreservationArtifact(t *testing.T, f *storefixture.Fixture, logical bool) ([]map[string]any, []byte) {
	t.Helper()

	if logical {
		data := logicalBytes(t, f.Store.(logicalStore))

		return logicalFrames(t, data), data
	}

	_, frames, data := backupFrames(t, f.Store.(backupStore), 0)

	return frames, data
}

func privacyPreservationEncode(t *testing.T, frames []map[string]any, logical bool) []byte {
	t.Helper()

	if logical {
		return encodeLogicalFrames(t, frames)
	}

	return encodeRestoreFrames(t, frames)
}

func privacyPreservationFrame(t *testing.T, frames []map[string]any, family canonicalsql.Family) map[string]any {
	t.Helper()

	for _, frame := range frames {
		if frame["family"] == string(family) || frame["record_type"] == string(family) {
			return frame
		}
	}

	t.Fatalf("missing original %s record", family)

	return nil
}

func privacyPreservationFrameValue(t *testing.T, frame map[string]any) map[string]any {
	t.Helper()

	encoded, err := json.Marshal(frame["envelope"])
	if err != nil {
		t.Fatal(err)
	}

	var data []byte
	if err := json.Unmarshal(encoded, &data); err != nil {
		t.Fatal(err)
	}

	envelope, err := memory.UnmarshalRecordEnvelope(data)
	if err != nil {
		t.Fatal(err)
	}

	var value map[string]any
	if err := json.Unmarshal(envelope.Payload, &value); err != nil {
		t.Fatal(err)
	}

	return value
}

func privacyPreservationSetFrame(t *testing.T, frame map[string]any, family canonicalsql.Family, value any) {
	t.Helper()

	data, err := canonicalsql.Encode(family, value)
	if err != nil {
		t.Fatal(err)
	}

	frame["envelope"] = data
}

// Restore spools are immediately unlinked. Inspect descriptors as well as
// names so an unlinked but unclosed spool cannot escape the cleanup assertion.
func privacyPreservationNoSpool(t *testing.T) {
	t.Helper()

	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if os.IsNotExist(err) {
			continue
		}

		if err != nil {
			t.Fatal(err)
		}

		if strings.Contains(target, "boyan-sql-restore-") {
			t.Fatalf("restore spool descriptor leaked: %s", target)
		}
	}
}
