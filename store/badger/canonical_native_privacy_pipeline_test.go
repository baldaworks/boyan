package badger //nolint:testpackage // Verifies native framing, incremental compatibility and restrictive versions.

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"path/filepath"
	"testing"
	"time"

	m "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
	"github.com/dgraph-io/badger/v4/pb"
)

func TestNativePrivacyFullThenIncrementalChain(t *testing.T) {
	source, first := privacyFixture(t)
	if _, err := source.ApplyCanonicalMutation(context.Background(), first); err != nil {
		t.Fatal(err)
	}

	var full bytes.Buffer

	version, err := source.WriteCanonicalBackup(context.Background(), &full, 0)
	if err != nil {
		t.Fatal(err)
	}

	destination := privacyRestoreDestination(t)
	if err := applyPrivacyRestore(destination, false, full.Bytes()); err != nil {
		t.Fatal(err)
	}

	next := incrementalPrivacyMutation(t, first)
	if _, err := source.ApplyCanonicalMutation(context.Background(), next); err != nil {
		t.Fatal(err)
	}

	incremental := nativePrivacyBackup(t, source, version)
	if err := applyPrivacyRestore(destination, false, incremental); err != nil {
		t.Fatal(err)
	}

	for _, mutation := range []m.CanonicalMutation{first, next} {
		if got := privacyState(t, destination, mutation.Scope, mutation.Sources[0].SourceID); got != sourcePrivacyStandard {
			t.Fatalf("incremental source=%s state=%s", mutation.Sources[0].SourceID, got)
		}

		assertPrivacyRestoredOutcome(t, destination, mutation, "native-standard")
	}

	state, err := destination.LoadScopeState(context.Background(), first.Scope)
	if err != nil || state.Version != 2 {
		t.Fatalf("incremental cursor: %+v %v", state, err)
	}
}

func incrementalPrivacyMutation(t *testing.T, first m.CanonicalMutation) m.CanonicalMutation {
	t.Helper()

	turn, err := m.NewTurn(first.Scope, m.ConversationRef{ConversationID: "conversation", AgentConversationID: "agent"},
		"incremental", first.Operation.CommittedAt.Add(time.Minute), "another safe memory", "OK")
	if err != nil {
		t.Fatal(err)
	}

	next := nativeTraceVisibilityMutation(t, turn)
	next.ExpectedScopeVersion = 1
	next.Operation.OperationID = "incremental-operation"
	next.Operation.Fingerprint = "incremental-operation"
	next.Items[0].ItemID = "incremental-item"
	next.Revisions[0].ItemID = next.Items[0].ItemID
	next.Revisions[0].RevisionID = "incremental-revision"
	next.Heads = []m.ItemHead{{ItemID: next.Items[0].ItemID, RevisionID: next.Revisions[0].RevisionID}}
	next.Sources[0].Payload = payloadRef("incremental-source", next.Payloads[0].Data)
	next.Payloads[0].Ref = next.Sources[0].Payload

	return next
}

func TestNativePrivacyInterleavedVersionsAndTombstones(t *testing.T) {
	source, mutation := privacyFixture(t)
	if _, err := source.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	stream := bytes.NewBuffer(nativePrivacyBackup(t, source, 0))
	sensitive := nativeAggregateKV(t, source, mutation, sourcePrivacySensitive, 100)
	standard := nativeAggregateKV(t, source, mutation, sourcePrivacyStandard, 200)
	unknown := nativeAggregateKV(t, source, mutation, sourcePrivacyUnknown, 150)
	scopeKey, _ := canonicalKey(mutation.Scope, badgerRecordSourcePrivacyScope, "coverage")
	scopeTrue := nativeScopeKV(t, mutation.Scope, true, 101)
	scopeFalse := nativeScopeKV(t, mutation.Scope, false, 250)
	privacyKey, _ := canonicalKey(mutation.Scope, badgerRecordSourcePrivacy, mutation.Sources[0].SourceID)

	lists := []*pb.KVList{
		{Kv: []*pb.KV{sensitive, scopeTrue}},
		{Kv: []*pb.KV{standard}},
		{Kv: []*pb.KV{unknown, scopeFalse}},
		{Kv: []*pb.KV{{Key: privacyKey, Version: 300, Meta: []byte{1}}, {Key: scopeKey, Version: 400, Meta: []byte{1}}}},
	}
	for _, list := range lists {
		if err := writeNativePrivacyFrame(stream, list); err != nil {
			t.Fatal(err)
		}
	}

	destination := privacyRestoreDestination(t)
	if err := applyPrivacyRestore(destination, false, stream.Bytes()); err != nil {
		t.Fatal(err)
	}

	if got := privacyState(t, destination, mutation.Scope, mutation.Sources[0].SourceID); got != sourcePrivacySensitive {
		t.Fatalf("restrictive version lost: %s", got)
	}

	assertRestoredPrivacyScopeIsolation(t, destination, mutation.Scope)
}

func assertRestoredPrivacyScopeIsolation(t *testing.T, destination *CanonicalStore, scope m.Scope) {
	t.Helper()

	err := destination.db.View(func(txn *badger.Txn) error {
		incomplete, err := privacyScopeIncomplete(txn, scope)
		if err == nil && !incomplete {
			t.Fatal("restrictive scope version lost")
		}

		group := scope
		group.Kind = m.ScopeKindGroup

		other, otherErr := privacyScopeIncomplete(txn, group)
		if other || otherErr != nil {
			t.Fatal("scope-kind taint crossed lane")
		}

		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func nativeAggregateKV(
	t *testing.T, store *CanonicalStore, mutation m.CanonicalMutation, state string, version uint64,
) *pb.KV {
	t.Helper()

	var record sourcePrivacy

	err := store.db.View(func(txn *badger.Txn) error {
		var err error

		record, _, err = loadSourcePrivacy(txn, mutation.Sources[0])

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	record.State = state
	record.StickyUnknown = state == sourcePrivacyUnknown
	key, _ := canonicalKey(mutation.Scope, badgerRecordSourcePrivacy, mutation.Sources[0].SourceID)

	return nativePrivacyKVFixture(t, key, badgerRecordSourcePrivacy, record, version)
}

func nativeScopeKV(t *testing.T, scope m.Scope, incomplete bool, version uint64) *pb.KV {
	t.Helper()

	key, _ := canonicalKey(scope, badgerRecordSourcePrivacyScope, "coverage")

	return nativePrivacyKVFixture(t, key, badgerRecordSourcePrivacyScope,
		sourcePrivacyScope{Version: sourcePrivacyVersion, Scope: scope, Incomplete: incomplete}, version)
}

func TestNativePrivacyFrameBoundsAndCleanup(t *testing.T) {
	for _, size := range []uint64{maxNativePrivacyFrameBytes + 1, math.MaxUint64} {
		t.Run("header", func(t *testing.T) {
			store := privacyRestoreDestination(t)

			var stream bytes.Buffer
			if err := binary.Write(&stream, binary.LittleEndian, size); err != nil {
				t.Fatal(err)
			}

			err := applyPrivacyRestore(store, false, stream.Bytes())

			code, _, ok := m.ClassifyError(err)
			if !ok || code != m.CodeLimitExceeded {
				t.Fatalf("frame bound failed: %v", err)
			}

			assertNoNativePrivacySpool(t, store)
		})
	}

	for _, data := range [][]byte{{1, 2, 3}, {3, 0, 0, 0, 0, 0, 0, 0, 0xff}, {1, 0, 0, 0, 0, 0, 0, 0, 0xff}} {
		store := privacyRestoreDestination(t)
		if err := applyPrivacyRestore(store, false, data); err == nil {
			t.Fatal("invalid/truncated frame accepted")
		}

		assertNoNativePrivacySpool(t, store)
	}

	value := bytes.Repeat([]byte("x"), 1<<20)
	kv := &pb.KV{Key: []byte("key"), Value: value}

	list := &pb.KVList{}
	for range 257 {
		list.Kv = append(list.Kv, kv)
	}

	var output bytes.Buffer
	if err := writeNativePrivacyFrame(&output, list); err == nil || output.Len() != 0 {
		t.Fatal("oversized expanded output allocated/written")
	}
}

func assertNoNativePrivacySpool(t *testing.T, store *CanonicalStore) {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(store.db.Opts().Dir, ".boyan-native-restore-*"))
	if err != nil || len(paths) != 0 {
		t.Fatalf("native spool leaked: %v %v", paths, err)
	}
}

func nativePrivacyKVFixture(t *testing.T, key []byte, kind string, value any, version uint64) *pb.KV {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}

	envelope, err := m.MarshalRecordEnvelope(kind, data)
	if err != nil {
		t.Fatal(err)
	}

	return &pb.KV{Key: key, Value: envelope, Version: version}
}
