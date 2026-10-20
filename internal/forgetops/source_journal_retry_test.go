package forgetops_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	"github.com/baldaworks/boyan/internal/forgetops"
	m "github.com/baldaworks/boyan/pkg/memory"
	db "github.com/dgraph-io/badger/v4"
)

// Resolving before journal replay fails here against the real payload-dependent
// migration resolver, both in process and after reopening the same database.
func TestSourceJournalRetryWithoutPayload(t *testing.T) {
	runSourceJournalAuthorities(t, testSourceJournalRetryWithoutPayload)
}

func testSourceJournalRetryWithoutPayload(t *testing.T, backend canonicalsqltest.Backend) {
	store, command, source, reopen := sourceJournalFixture(t, false, backend)
	adapter := sourceJournalAdapter(t, store)
	outcome, err := adapter.ForgetSource(context.Background(), command)
	sourceJournalNoError(t, err)

	if outcome.Kind != m.ForgetKindSource || len(outcome.Sources) != 1 || outcome.Sources[0] != command.Source {
		t.Fatalf("first erase did not select imported source: %+v", outcome)
	}

	sourceJournalPayloadAbsent(t, store, source.Payload)
	sourceJournalStructurePresent(t, store, source)

	journal := sourceJournalLoad(t, store, command)
	if journal.Stage != m.ScopeEraseComplete {
		t.Fatalf("original journal identity/boundary lost: %+v", journal)
	}

	sourceJournalIdentity(t, journal, source.SourceID)

	if _, err = store.CanonicalSourceID(context.Background(), command.Source.Scope, command.Source); err == nil {
		t.Fatal("fixture does not exercise payload-dependent resolution after erase")
	}

	before := sourceJournalBytes(t, store)
	command.ForgottenAt = command.ForgottenAt.Add(time.Hour)
	replay, err := adapter.ForgetSource(context.Background(), command)
	sourceJournalNoError(t, err)

	if !reflect.DeepEqual(replay, outcome) || !reflect.DeepEqual(sourceJournalBytes(t, store), before) {
		t.Fatal("replay changed original outcome or durable bytes")
	}

	sourceJournalNoError(t, store.Close())
	store, err = reopen()
	sourceJournalNoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	replay, err = sourceJournalAdapter(t, store).ForgetSource(context.Background(), command)
	sourceJournalNoError(t, err)

	if !reflect.DeepEqual(replay, outcome) || !reflect.DeepEqual(sourceJournalBytes(t, store), before) {
		t.Fatal("restart replay changed original outcome or durable bytes")
	}
}

// Returning a persisted Outcome without running the journal would leave the
// canonical fence and unfinished scrub behind.
func TestSourceJournalRetryFinishesPendingOutcome(t *testing.T) {
	runSourceJournalAuthorities(t, testSourceJournalRetryFinishesPendingOutcome)
}

func testSourceJournalRetryFinishesPendingOutcome(t *testing.T, backend canonicalsqltest.Backend) {
	store, command, source, _ := sourceJournalFixture(t, false, backend)
	request := sourceJournalRequest(t, command, source.SourceID, 1)
	_, err := store.BeginScopeErase(context.Background(), request)
	sourceJournalNoError(t, err)

	var pending m.ScopeEraseJournal
	for range 100 {
		pending, err = store.AdvanceScopeErase(context.Background(), request.Scope, request.OperationID)
		sourceJournalNoError(t, err)

		if pending.Stage == m.ScopeEraseScrubMessages {
			break
		}
	}

	if pending.Stage != m.ScopeEraseScrubMessages || pending.Outcome == nil {
		t.Fatal("fixture did not reach durable outcome with unfinished canonical work")
	}

	sourceJournalPayloadAbsent(t, store, source.Payload)

	command.ForgottenAt = command.ForgottenAt.Add(time.Hour)
	outcome, err := sourceJournalAdapter(t, store).ForgetSource(context.Background(), command)
	sourceJournalNoError(t, err)

	complete := sourceJournalLoad(t, store, command).Stage == m.ScopeEraseComplete
	if !reflect.DeepEqual(outcome, *pending.Outcome) || !complete {
		t.Fatal("retry did not finish canonical work with original outcome")
	}

	pendingJournals, _, err := store.ListPendingScopeErases(context.Background(), nil, 10)
	sourceJournalNoError(t, err)

	if len(pendingJournals) != 0 {
		t.Fatal("retry left an unfinished erase")
	}
}

// Build through canonical mutation; the legacy SourceRef maps to an explicit
// nonnative SourceID, never a guess.
func sourceJournalFixture(t *testing.T, native bool, backend canonicalsqltest.Backend) (
	*sourceJournalStore, m.ForgetSourceCommand, m.CanonicalSourceRecord, func() (*sourceJournalStore, error),
) {
	t.Helper()

	scope := m.Scope{Key: "test:source-journal", Kind: m.ScopeKindPersonal}
	at := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	turn, err := m.NewTurn(scope, m.ConversationRef{ConversationID: "conversation", AgentConversationID: "agent"},
		"turn", at.Add(-time.Hour), "private text", "acknowledged")
	sourceJournalNoError(t, err)

	command := m.ForgetSourceCommand{SchemaVersion: m.DerivedSchemaVersionV1,
		Source: m.SourceRef{Scope: scope, ExportID: turn.ExportID,
			ConversationID: turn.Conversation.ConversationID, SourceTurnID: turn.SourceTurnID}, ForgottenAt: at}
	archived := m.SourceRecord{
		SchemaVersion: m.DerivedSchemaVersionV1, Ref: command.Source, State: m.SourceStateActive, Turn: &turn,
	}
	sourceJournalNoError(t, archived.Validate())
	data, err := json.Marshal(archived)
	sourceJournalNoError(t, err)

	digest := sha256.Sum256(data)

	source := m.CanonicalSourceRecord{SourceID: command.Source.ExportID, Scope: scope,
		Sensitivity: m.SensitivityStandard, Retention: m.RetentionClassStandard,
		Payload: m.PayloadRef{ID: "source-payload", Digest: hex.EncodeToString(digest[:]), ByteSize: uint32(len(data))}}
	if native {
		source.SourceID = m.TurnSourceID(command.Source.ExportID)
	}

	store, reopen := openSourceJournalStore(t, backend)
	t.Cleanup(func() { _ = store.Close() })

	_, err = store.ApplyCanonicalMutation(context.Background(), m.CanonicalMutation{
		SchemaVersion: m.CanonicalSchemaVersionV1, Scope: scope,
		Operation: m.OperationRecord{
			OperationID: "imported-operation", Fingerprint: "imported-fingerprint",
			CommittedAt: command.ForgottenAt.Add(-time.Hour),
		},
		Sources: []m.CanonicalSourceRecord{source}, Payloads: []m.CanonicalPayload{{Ref: source.Payload, Data: data}},
	})
	sourceJournalNoError(t, err)

	resolved, err := store.CanonicalSourceID(context.Background(), scope, command.Source)
	sourceJournalNoError(t, err)

	if resolved != source.SourceID {
		t.Fatalf("imported source resolved to %q, want %q", resolved, source.SourceID)
	}

	return store, command, source, reopen
}

func sourceJournalRequest(
	t *testing.T, command m.ForgetSourceCommand, sourceID string, baseline uint64,
) m.ScopeEraseRequest {
	t.Helper()

	operationID, err := m.CanonicalForgetOperationID(m.ForgetKindSource, command.Source.Scope, command.Source, "")
	sourceJournalNoError(t, err)

	return m.ScopeEraseRequest{Kind: m.ForgetKindSource, Scope: command.Source.Scope, SourceRef: command.Source,
		SourceID: sourceID, ExpectedScopeVersion: baseline, ForgottenAt: command.ForgottenAt, OperationID: operationID,
		Fingerprint: m.CanonicalForgetFingerprint(operationID, command.Source.Scope, m.ForgetKindSource, command.Source, "")}
}

func sourceJournalAdapter(t *testing.T, store *sourceJournalStore) *forgetops.CanonicalForgetAdapter {
	t.Helper()

	adapter, err := forgetops.NewCanonicalForgetAdapter(store)
	sourceJournalNoError(t, err)

	return adapter
}

func sourceJournalLoad(t *testing.T, store *sourceJournalStore, command m.ForgetSourceCommand) m.ScopeEraseJournal {
	t.Helper()
	request := sourceJournalRequest(t, command, "unused", 0)
	journal, found, err := store.LoadScopeErase(context.Background(), command.Source.Scope, request.OperationID)
	sourceJournalNoError(t, err)

	if !found {
		t.Fatal("journal missing")
	}

	return journal
}

func sourceJournalPayloadAbsent(t *testing.T, store *sourceJournalStore, ref m.PayloadRef) {
	t.Helper()

	if _, err := store.LoadPayload(context.Background(), ref); err == nil {
		t.Fatal("source payload survived canonical scrub")
	}
}

func sourceJournalBytes(t *testing.T, store *sourceJournalStore) map[string]string {
	t.Helper()

	if store.database != nil {
		return sourceJournalSQLBytes(t, store.database)
	}

	result := make(map[string]string)
	err := store.legacy.DB().View(func(txn *db.Txn) error {
		iterator := txn.NewIterator(db.DefaultIteratorOptions)
		defer iterator.Close()

		for iterator.Rewind(); iterator.Valid(); iterator.Next() {
			item := iterator.Item()

			value, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}

			result[string(item.Key())] = string(value)
		}

		return nil
	})
	sourceJournalNoError(t, err)

	return result
}

func sourceJournalNoError(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
}

func sourceJournalIdentity(t *testing.T, journal m.ScopeEraseJournal, sourceID string) {
	t.Helper()

	if journal.SourceID != sourceID || journal.BaselineVersion != 1 {
		t.Fatalf("original journal identity/boundary lost: %+v", journal)
	}
}

func sourceJournalStructurePresent(t *testing.T, store *sourceJournalStore, source m.CanonicalSourceRecord) {
	t.Helper()

	for _, encoded := range sourceJournalEnvelopes(t, store) {
		envelope, err := m.UnmarshalRecordEnvelope([]byte(encoded))
		sourceJournalNoError(t, err)

		if envelope.RecordType != "source" {
			continue
		}

		var structural m.CanonicalSourceRecord
		sourceJournalNoError(t, json.Unmarshal(envelope.Payload, &structural))

		if structural == source {
			return
		}
	}

	t.Fatal("erase removed the original structural source record")
}
