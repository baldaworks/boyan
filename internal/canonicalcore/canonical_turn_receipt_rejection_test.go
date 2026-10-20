package canonicalcore_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	m "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func TestTurnHTTPReceiptRejectsUnboundOrUnprovenReplay(t *testing.T) {
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		for _, kind := range []string{"changed", "wrong-kind", "wrong-export", "wrong-scope", "absent",
			"unproven", "corrupt", "forgotten-proofless"} {
			t.Run(kind, func(t *testing.T) { testTurnReceiptRejection(t, kind, factory) })
		}
	})
}

func testTurnReceiptRejection(t *testing.T, kind string, factory coreFactory) {
	t.Helper()

	f := newTurnReceiptFixture(t, factory)
	if response := f.post(t, f.turn); response.Code != http.StatusOK {
		t.Fatalf("initial: %s", response.Body)
	}

	prepareTurnReceiptRejection(t, f, kind)
	original := f.recordExceptCorrupt(t, kind)
	before, err := f.store.LoadScopeState(context.Background(), f.turn.Scope)
	requireNoError(t, err, "before: %v")

	retry := f.turn
	if kind == "changed" {
		retry = changedIdentityTurn(retry)
	}

	response := f.post(t, retry)

	want := http.StatusConflict
	if kind == "wrong-scope" || kind == "corrupt" {
		want = http.StatusServiceUnavailable
	}

	if response.Code != want || strings.Contains(response.Body.String(), "receipt secret sentinel") {
		t.Fatalf("unsafe or accepted replay: %d %s", response.Code, response.Body)
	}

	assertRejectedTurnReceiptUnchanged(t, f, kind, before, original)
}

func assertRejectedTurnReceiptUnchanged(
	t *testing.T, f *turnReceiptFixture, kind string, before m.ScopeState, original m.IngestSequenceRecord,
) {
	t.Helper()

	after, err := f.store.LoadScopeState(context.Background(), f.turn.Scope)
	requireNoError(t, err, "after: %v")

	if before != after || f.calls != 1 || f.processCalls != 1 ||
		!reflect.DeepEqual(original, f.recordExceptCorrupt(t, kind)) {
		t.Fatal("rejected replay changed state or entered processor")
	}
}

func (f *turnReceiptFixture) recordExceptCorrupt(t *testing.T, kind string) m.IngestSequenceRecord {
	t.Helper()

	if kind == "wrong-scope" {
		return m.IngestSequenceRecord{}
	}

	return f.record(t)
}

func prepareTurnReceiptRejection(t *testing.T, f *turnReceiptFixture, kind string) {
	t.Helper()

	switch kind {
	case "wrong-kind", "wrong-export", "wrong-scope":
		alterTurnReceipt(t, f, kind)
	case "absent":
		deleteTurnReceiptRecord(t, f, "turn-certificate", f.turn.SourceID)
		deleteTurnReceiptRecord(t, f, "source", f.turn.SourceID)
	case "unproven":
		deleteTurnReceiptRecord(t, f, "turn-certificate", f.turn.SourceID)
		deleteTurnReceiptRecord(t, f, "message", f.turn.Messages[0].MessageID)
	case "corrupt":
		if f.factory != nil {
			alterCoreSQLEnvelope(
				t,
				f.store,
				"turn_certificate",
				"scope_key=$2 AND scope_kind=$3 AND id=$4",
				[]byte("corrupt"),
				[]byte(f.turn.Scope.Key),
				[]byte(f.turn.Scope.Kind),
				[]byte(f.turn.SourceID),
			)

			return
		}

		key := turnReceiptRecordKey(t, f, "turn-certificate", f.turn.SourceID)
		requireNoError(t, f.store.(interface{ DB() *badger.DB }).DB().Update(func(txn *badger.Txn) error {
			return txn.Set(key, []byte("corrupt"))
		}), "corrupt: %v")
	case "forgotten-proofless":
		forgetTurnReceiptFixture(t, f)
		deleteTurnReceiptRecord(t, f, "turn-certificate", f.turn.SourceID)
	}
}

func alterTurnReceipt(t *testing.T, f *turnReceiptFixture, kind string) {
	t.Helper()
	record := f.record(t)

	switch kind {
	case "wrong-kind":
		record.Outcome.Kind = m.IngestKindBoundary
	case "wrong-export":
		record.Outcome.ExportID = "another-export"
	case "wrong-scope":
		record.Scope.Kind = m.ScopeKindGroup
		record.Outcome.Scope = record.Scope
	}

	writeTurnReceiptFixture(t, f, record)
}

func writeTurnReceiptFixture(t *testing.T, f *turnReceiptFixture, record m.IngestSequenceRecord) {
	t.Helper()

	payload, err := json.Marshal(record)
	requireNoError(t, err, "receipt JSON: %v")
	value, err := m.MarshalRecordEnvelope("ingest-sequence", payload)
	requireNoError(t, err, "receipt envelope: %v")

	if f.factory != nil {
		alterCoreSQLEnvelope(
			t,
			f.store,
			"ingest_sequence",
			"scope_key=$2 AND scope_kind=$3 AND producer_id=$4 AND sequence_u64=$5",
			value,
			[]byte(
				f.turn.Scope.Key,
			),
			[]byte(f.turn.Scope.Kind),
			[]byte("old-producer"),
			binary.BigEndian.AppendUint64(nil, 1),
		)

		return
	}

	prefix, err := m.TupleKey(0x53, 1, "ingest-sequence", f.turn.Scope.Key, string(f.turn.Scope.Kind), "old-producer")
	requireNoError(t, err, "sequence key: %v")

	key := binary.BigEndian.AppendUint64(prefix, 1)

	requireNoError(
		t,
		f.store.(interface{ DB() *badger.DB }).DB().Update(func(txn *badger.Txn) error { return txn.Set(key, value) }),
		"alter receipt: %v",
	)
}

func turnReceiptRecordKey(t *testing.T, f *turnReceiptFixture, kind, id string) []byte {
	t.Helper()

	key, err := m.TupleKey(0x53, 1, kind, f.turn.Scope.Key, string(f.turn.Scope.Kind), id)
	requireNoError(t, err, "key: %v")

	return key
}

func deleteTurnReceiptRecord(t *testing.T, f *turnReceiptFixture, kind, id string) {
	t.Helper()

	if f.factory != nil {
		deleteCoreSQLRecord(t, f.store, coreSQLKey(canonicalsql.Family(kind), f.turn.Scope, []byte(id)))

		return
	}

	key := turnReceiptRecordKey(t, f, kind, id)
	requireNoError(
		t,
		f.store.(interface{ DB() *badger.DB }).DB().Update(func(txn *badger.Txn) error { return txn.Delete(key) }),
		"delete proof: %v",
	)
}
