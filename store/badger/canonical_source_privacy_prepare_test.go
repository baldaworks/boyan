package badger //nolint:testpackage // Exercises bounded preparation and transactional readiness.

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"testing"

	m "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func makePrivacyHistoryOld(t *testing.T, store *CanonicalStore, mutation m.CanonicalMutation) {
	t.Helper()

	key, _ := canonicalKey(mutation.Scope, badgerRecordSourcePrivacy, mutation.Sources[0].SourceID)

	if err := store.db.Update(func(txn *badger.Txn) error {
		if err := txn.Delete(key); err != nil {
			return err
		}

		return txn.Delete(sourcePrivacyReadinessKey())
	}); err != nil {
		t.Fatal(err)
	}
}

func preparePrivacy(t *testing.T, store *CanonicalStore, ctx context.Context) error {
	t.Helper()

	preparer, ok := any(store).(interface {
		PrepareSourcePrivacy(ctx context.Context) error
	})
	if !ok {
		t.Fatal("historical source privacy preparation is unavailable")
	}

	return preparer.PrepareSourcePrivacy(ctx)
}

func TestSourcePrivacyUnpreparedTraceReturnsUnavailable(t *testing.T) {
	store, mutation := privacyFixture(t)
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	makePrivacyHistoryOld(t, store, mutation)

	reader, err := NewCanonicalReader(store)
	if err != nil {
		t.Fatal(err)
	}

	response, err := reader.Trace(context.Background(), privacyTraceRequest(mutation))
	if code, _, ok := m.ClassifyError(err); !ok || code != m.CodeUnavailable ||
		len(response.Revisions) != 0 || len(response.Sources) != 0 {
		t.Fatalf("unprepared trace served graph: %+v %v", response, err)
	}
}

func TestPrepareSourcePrivacyReconstructsAuthoritativeHistory(t *testing.T) {
	cases := []struct{ kind, want string }{
		{"native-standard", "standard"}, {"native-certified", "standard"}, {"identity-only", "unknown"},
		{"migrated-standard", "standard"}, {"migrated-embedded-only", "standard"},
		{"mixed-same", "sensitive"}, {"mixed-separate", "sensitive"},
		{"opaque-source", "unknown"}, {"incomplete-evidence", "unknown"},
	}
	for _, test := range cases {
		t.Run(test.kind, func(t *testing.T) {
			store, mutation := historicalPrivacyFixture(t, test.kind)
			if err := preparePrivacy(t, store, context.Background()); err != nil {
				t.Fatal(err)
			}

			if got := privacyState(t, store, mutation.Scope, mutation.Sources[0].SourceID); got != test.want {
				t.Fatalf("history classification=%s, want %s", got, test.want)
			}

			if test.want == "standard" {
				assertPreparedStandardTrace(t, store, mutation)
			}
		})
	}
}

func historicalPrivacyFixture(t *testing.T, kind string) (*CanonicalStore, m.CanonicalMutation) {
	t.Helper()
	store, mutation := privacyFixture(t)
	configurePrivacyHistory(t, &mutation, kind)

	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	makePrivacyHistoryOld(t, store, mutation)

	if kind == "migrated-embedded-only" {
		dropHistoricalMessageRecords(t, store, mutation)
	}

	return store, mutation
}

func configurePrivacyHistory(t *testing.T, mutation *m.CanonicalMutation, kind string) {
	t.Helper()

	switch kind {
	case "native-certified", "identity-only":
		certifyPrivacyHistory(t, mutation)

		if kind == "identity-only" {
			mutation.Revisions = nil
			mutation.Heads = nil
		}
	case "migrated-standard", "migrated-embedded-only":
		embedPrivacyHistory(t, mutation)
	case "mixed-same", "mixed-separate":
		appendSensitivePrivacyHistory(mutation, kind == "mixed-separate")
	case "opaque-source":
		mutation.Sources[0].Payload = payloadRef("opaque-source", []byte("opaque"))
		mutation.Payloads[0] = m.CanonicalPayload{Ref: mutation.Sources[0].Payload, Data: []byte("opaque")}
	case "incomplete-evidence":
		mutation.Revisions[0].Evidence[0].TextDigest = ""
	}
}

func certifyPrivacyHistory(t *testing.T, mutation *m.CanonicalMutation) {
	t.Helper()
	turn := canonicalReaderTestTurn(t, mutation.Scope, mutation.Operation.CommittedAt)

	fingerprint, err := m.CanonicalTurnFingerprint(turn)
	if err != nil {
		t.Fatal(err)
	}

	certificate := m.CanonicalTurnCertificate{Version: m.CanonicalTurnFingerprintVersion,
		SourceID: turn.SourceID, Fingerprint: fingerprint}
	for _, message := range turn.Messages {
		certificate.MessageIDs = append(certificate.MessageIDs, message.MessageID)
	}

	mutation.TurnIdentity = &m.CanonicalTurnIdentityCommit{Certificate: certificate, Turn: turn}
}

func embedPrivacyHistory(t *testing.T, mutation *m.CanonicalMutation) {
	t.Helper()
	turn := canonicalReaderTestTurn(t, mutation.Scope, mutation.Operation.CommittedAt)

	payload, err := json.Marshal(privacyEmbeddedRecord(turn))
	if err != nil {
		t.Fatal(err)
	}

	mutation.Sources[0].Payload = payloadRef("migrated-source", payload)
	mutation.Payloads[0] = m.CanonicalPayload{Ref: mutation.Sources[0].Payload, Data: payload}
}

func appendSensitivePrivacyHistory(mutation *m.CanonicalMutation, separate bool) {
	sensitive := mutation.Revisions[0]
	sensitive.RevisionID = "sensitive-history"
	sensitive.Sensitivity = m.SensitivitySensitive
	sensitive.Evidence = append([]m.EvidenceRef(nil), sensitive.Evidence...)

	if separate {
		mutation.Messages[1].Role = m.MessageRoleUser
		sensitive.Evidence[0] = m.EvidenceRef{SourceID: mutation.Sources[0].SourceID,
			MessageID: mutation.Messages[1].MessageID, Role: m.MessageRoleUser, StartByte: 0,
			EndByte: mutation.Messages[1].Payload.ByteSize, TextDigest: mutation.Messages[1].Payload.Digest,
			AssertionMode: m.AssertionModeUser}
	}

	mutation.Revisions = append(mutation.Revisions, sensitive)
}

func dropHistoricalMessageRecords(t *testing.T, store *CanonicalStore, mutation m.CanonicalMutation) {
	t.Helper()

	if err := store.db.Update(func(txn *badger.Txn) error {
		for _, message := range mutation.Messages {
			key, _ := canonicalKey(mutation.Scope, badgerRecordMessage, message.MessageID)
			if err := txn.Delete(key); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func assertPreparedStandardTrace(t *testing.T, store *CanonicalStore, mutation m.CanonicalMutation) {
	t.Helper()

	reader, err := NewCanonicalReader(store)
	if err != nil {
		t.Fatal(err)
	}

	response, err := reader.Trace(context.Background(), privacyTraceRequest(mutation))
	if err != nil || len(response.Sources) != 1 {
		t.Fatalf("standard trace unavailable: %+v %v", response, err)
	}
}

func privacyTraceRequest(mutation m.CanonicalMutation) m.TraceRequest {
	return m.TraceRequest{Scope: mutation.Scope, Root: m.RevisionRef{
		ItemID: mutation.Items[0].ItemID, RevisionID: mutation.Revisions[0].RevisionID}, MaxNodes: 10}
}

func TestPrepareSourcePrivacyCancellationRestartsBoundedPass(t *testing.T) {
	store, mutation := privacyFixture(t)
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	growPrivacyHistory(t, store, mutation)
	makePrivacyHistoryOld(t, store, mutation)

	ctx, cancel := context.WithCancel(context.Background())
	maximum := 0
	visited := 0

	store.afterSourcePrivacyBatch = func(kind string, count int) error {
		if count > maximum {
			maximum = count
		}

		if kind == badgerRecordRevision {
			visited += count

			cancel()
		}

		return nil
	}
	if err := preparePrivacy(t, store, ctx); err == nil {
		t.Fatal("canceled pass published readiness")
	}

	if maximum > sourcePrivacyBatchSize || visited != sourcePrivacyBatchSize {
		t.Fatalf("unbounded pass: max=%d visited=%d", maximum, visited)
	}

	if err := store.db.View(requireSourcePrivacyReady); err == nil {
		t.Fatal("partial pass authorized trace")
	}

	store.afterSourcePrivacyBatch = nil
	if err := preparePrivacy(t, store, context.Background()); err != nil {
		t.Fatal(err)
	}

	if got := privacyState(t, store, mutation.Scope, mutation.Sources[0].SourceID); got != "standard" {
		t.Fatalf("restart classification=%s", got)
	}
}

func TestPrepareSourcePrivacyOpaqueHistoryTaintsOnlyExactScope(t *testing.T) {
	store, mutation := privacyFixture(t)
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	other := mutation
	other.Scope.Kind = m.ScopeKindGroup

	other.Sources = append([]m.CanonicalSourceRecord(nil), mutation.Sources...)
	other.Sources[0].Scope = other.Scope

	other.Items = append([]m.MemoryItem(nil), mutation.Items...)

	other.Items[0].Scope = other.Scope
	if _, err := store.ApplyCanonicalMutation(context.Background(), other); err != nil {
		t.Fatal(err)
	}

	if err := store.db.Update(func(txn *badger.Txn) error {
		key, _ := canonicalKey(mutation.Scope, badgerRecordRevision, "opaque-history")

		return putCanonicalRecord(txn, key, badgerRecordRevision, map[string]any{"legacy": "opaque"})
	}); err != nil {
		t.Fatal(err)
	}

	makePrivacyHistoryOld(t, store, mutation)

	if err := preparePrivacy(t, store, context.Background()); err != nil {
		t.Fatal(err)
	}

	if got := privacyState(t, store, mutation.Scope, mutation.Sources[0].SourceID); got != "unknown" {
		t.Fatalf("opaque scope blessed: %s", got)
	}

	if got := privacyState(t, store, other.Scope, other.Sources[0].SourceID); got != "standard" {
		t.Fatalf("unrelated scope tainted: %s", got)
	}
	// A fresh source cannot launder an unattributable historic classification.
	fresh := privacyFreshSubsetMutation(mutation)

	if _, err := store.ApplyCanonicalMutation(context.Background(), fresh); err != nil {
		t.Fatal(err)
	}

	if got := privacyState(t, store, fresh.Scope, "fresh-source"); got != "unknown" {
		t.Fatalf("scope history laundered: %s", got)
	}
}

func TestPrepareSourcePrivacyCorruptionFailsWithoutReadiness(t *testing.T) {
	for _, kind := range []string{
		"source-scope", "revision-id", "oversized-envelope", "scope-metadata", "privacy-combination",
	} {
		t.Run(kind, func(t *testing.T) {
			store, mutation := privacyFixture(t)
			if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
				t.Fatal(err)
			}

			makePrivacyHistoryOld(t, store, mutation)

			if err := store.db.Update(func(txn *badger.Txn) error {
				return corruptPrivacyHistory(txn, mutation, kind)
			}); err != nil {
				t.Fatal(err)
			}

			if err := preparePrivacy(t, store, context.Background()); err == nil {
				t.Fatal("corrupt history prepared")
			}

			if err := store.db.View(requireSourcePrivacyReady); err == nil {
				t.Fatal("failed pass published readiness")
			}
		})
	}
}

func TestPrepareSourcePrivacyMissingNativeMembershipStaysUnknown(t *testing.T) {
	store, mutation := privacyFixture(t)
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	makePrivacyHistoryOld(t, store, mutation)

	if err := store.db.Update(func(txn *badger.Txn) error {
		key, _ := canonicalKey(mutation.Scope, badgerRecordMessage, mutation.Revisions[0].Evidence[0].MessageID)

		return txn.Delete(key)
	}); err != nil {
		t.Fatal(err)
	}

	if err := preparePrivacy(t, store, context.Background()); err != nil {
		t.Fatal(err)
	}

	if got := privacyState(t, store, mutation.Scope, mutation.Sources[0].SourceID); got != "unknown" {
		t.Fatalf("missing native membership certified: %s", got)
	}
}

func TestSourcePrivacyNewUncertifiedMembershipRecorded(t *testing.T) {
	store, mutation := privacyFixture(t)
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	if err := store.db.View(func(txn *badger.Txn) error {
		record, _, err := loadSourcePrivacy(txn, mutation.Sources[0])
		if err != nil {
			return err
		}

		if len(record.MessageIDs) != len(mutation.Messages) {
			t.Fatalf("initial membership missing: %v", record.MessageIDs)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareSourcePrivacyRebuildsEdgesAndRetainsCompletedRestart(t *testing.T) {
	store, mutation := privacyFixture(t)
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	makePrivacyHistoryOld(t, store, mutation)
	edge, _ := badgerProvenanceKey(mutation.Scope, badgerRecordSourceRevision,
		mutation.Sources[0].SourceID, mutation.Revisions[0].RevisionID)

	if err := store.db.Update(func(txn *badger.Txn) error { return txn.Delete(edge) }); err != nil {
		t.Fatal(err)
	}

	if err := preparePrivacy(t, store, context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := store.db.View(func(txn *badger.Txn) error {
		_, err := txn.Get(edge)

		return err
	}); err != nil {
		t.Fatalf("evidence index not rebuilt: %v", err)
	}

	directory := store.db.Opts().Dir
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenCanonicalStore(directory)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = reopened.Close() })

	if err := reopened.db.View(requireSourcePrivacyReady); err != nil {
		t.Fatalf("completed readiness lost: %v", err)
	}

	if got := privacyState(t, reopened, mutation.Scope, mutation.Sources[0].SourceID); got != "standard" {
		t.Fatalf("restart state=%s", got)
	}
}

func TestPrepareSourcePrivacyForceCannotLaunderCompletedUnknown(t *testing.T) {
	store, mutation := privacyFixture(t)
	initial := mutation
	initial.Revisions = nil

	initial.Heads = nil
	if _, err := store.ApplyCanonicalMutation(context.Background(), initial); err != nil {
		t.Fatal(err)
	}

	mutation.ExpectedScopeVersion = 1
	mutation.Operation.OperationID = "later-subset"

	mutation.Operation.Fingerprint = "later-subset"
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	store.maintenanceMu.Lock()

	err := store.db.Update(func(txn *badger.Txn) error { return txn.Delete(sourcePrivacyReadinessKey()) })
	if err == nil {
		err = store.prepareSourcePrivacyLocked(context.Background())
	}
	store.maintenanceMu.Unlock()

	if err != nil {
		t.Fatal(err)
	}

	if got := privacyState(t, store, mutation.Scope, mutation.Sources[0].SourceID); got != "unknown" {
		t.Fatalf("completed unknown laundered: %s", got)
	}
}

func TestPrepareSourcePrivacyExcludesWritersUntilCompletion(t *testing.T) {
	store, mutation := privacyFixture(t)
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	makePrivacyHistoryOld(t, store, mutation)

	entered := make(chan struct{})
	release := make(chan struct{})

	var once sync.Once

	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()

	store.afterSourcePrivacyBatch = func(kind string, _ int) error {
		if kind == badgerRecordRevision {
			close(entered)
			<-release
		}

		return nil
	}

	prepared := make(chan error, 1)
	go func() { prepared <- store.PrepareSourcePrivacy(context.Background()) }()

	<-entered

	if err := store.db.View(requireSourcePrivacyReady); err == nil {
		t.Fatal("partial pass published readiness")
	}

	attempted := make(chan struct{})
	written := make(chan error, 1)

	go func() {
		close(attempted)

		_, err := store.ApplyCanonicalMutation(context.Background(), mutation)
		written <- err
	}()

	<-attempted

	select {
	case err := <-written:
		t.Fatalf("writer escaped preparation exclusion: %v", err)
	default:
	}

	unblock()

	if err := <-prepared; err != nil {
		t.Fatal(err)
	}

	if err := <-written; err != nil {
		t.Fatal(err)
	}
}

func TestPrepareSourcePrivacyMissingAuthoritativeContentStaysUnknown(t *testing.T) {
	for _, component := range []string{"message-blob", "item"} {
		t.Run(component, func(t *testing.T) {
			store, mutation := privacyFixture(t)
			if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
				t.Fatal(err)
			}

			makePrivacyHistoryOld(t, store, mutation)

			if err := store.db.Update(func(txn *badger.Txn) error {
				if component == "message-blob" {
					key, _ := payloadRecordKey(mutation.Messages[0].Payload)

					return txn.Delete(key)
				}

				key, _ := canonicalKey(mutation.Scope, badgerRecordItem, mutation.Revisions[0].ItemID)

				return txn.Delete(key)
			}); err != nil {
				t.Fatal(err)
			}

			if err := preparePrivacy(t, store, context.Background()); err != nil {
				t.Fatal(err)
			}

			if got := privacyState(t, store, mutation.Scope, mutation.Sources[0].SourceID); got != "unknown" {
				t.Fatalf("missing authority certified: %s", got)
			}
		})
	}
}

func growPrivacyHistory(t *testing.T, store *CanonicalStore, mutation m.CanonicalMutation) {
	t.Helper()

	if err := store.db.Update(func(txn *badger.Txn) error {
		for i := 0; i < sourcePrivacyBatchSize*3+1; i++ {
			revision := mutation.Revisions[0]

			revision.RevisionID = "historic-" + strconv.Itoa(i)
			if err := putBadgerCanonicalRecord(txn, mutation.Scope,
				badgerRecordRevision, revision.RevisionID, revision); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func corruptPrivacyHistory(txn *badger.Txn, mutation m.CanonicalMutation, kind string) error {
	switch kind {
	case "privacy-combination":
		record := sourcePrivacy{Version: sourcePrivacyVersion, CoverageVersion: sourcePrivacyVersion,
			Scope: mutation.Scope, SourceID: mutation.Sources[0].SourceID, Payload: mutation.Sources[0].Payload,
			State: sourcePrivacyStandard, StickyUnknown: true}

		return writeSourcePrivacy(txn, record)
	case "scope-metadata":
		key, _ := canonicalKey(mutation.Scope, badgerRecordSourcePrivacyScope, "coverage")
		record := sourcePrivacyScope{Version: sourcePrivacyVersion,
			Scope: m.Scope{Key: mutation.Scope.Key, Kind: m.ScopeKindGroup}}

		return putCanonicalRecord(txn, key, badgerRecordSourcePrivacyScope, record)
	case "source-scope":
		source := mutation.Sources[0]
		source.Scope.Kind = m.ScopeKindGroup
		key, _ := canonicalKey(mutation.Scope, badgerRecordSource, source.SourceID)

		return putCanonicalRecord(txn, key, badgerRecordSource, source)
	case "oversized-envelope":
		key, _ := canonicalKey(mutation.Scope, badgerRecordRevision, mutation.Revisions[0].RevisionID)

		return txn.Set(key, bytes.Repeat([]byte("x"), maxCanonicalValueBytes+1))
	default:
		key, _ := canonicalKey(mutation.Scope, badgerRecordRevision, mutation.Revisions[0].RevisionID)
		revision := mutation.Revisions[0]
		revision.RevisionID = ""

		return putCanonicalRecord(txn, key, badgerRecordRevision, revision)
	}
}

//nolint:cyclop // The same fixture verifies preparation, mutation, retry and retained incomplete authority.
func TestPrepareSourcePrivacyUnsupportedDanglingEvidenceCannotLaunder(t *testing.T) {
	store, mutation := privacyFixture(t)
	historical := mutation.Revisions[0]
	historical.SchemaVersion = "unsupported-history"
	historical.RevisionID = "unsupported-history"

	if err := store.db.Update(func(txn *badger.Txn) error {
		key, _ := canonicalKey(mutation.Scope, badgerRecordRevision, historical.RevisionID)
		if err := putCanonicalRecord(txn, key, badgerRecordRevision, historical); err != nil {
			return err
		}

		return txn.Delete(sourcePrivacyReadinessKey())
	}); err != nil {
		t.Fatal(err)
	}

	if err := preparePrivacy(t, store, context.Background()); err != nil {
		t.Fatal(err)
	}

	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	if got := privacyState(t, store, mutation.Scope, mutation.Sources[0].SourceID); got != "unknown" {
		t.Fatalf("unsupported dangling history laundered: %s", got)
	}

	store.maintenanceMu.Lock()

	err := store.db.Update(func(txn *badger.Txn) error { return txn.Delete(sourcePrivacyReadinessKey()) })
	if err == nil {
		err = store.prepareSourcePrivacyLocked(context.Background())
	}
	store.maintenanceMu.Unlock()

	if err != nil {
		t.Fatal(err)
	}

	if err := store.db.View(func(txn *badger.Txn) error {
		incomplete, err := privacyScopeIncomplete(txn, mutation.Scope)
		if err != nil {
			return err
		}

		if !incomplete {
			t.Fatal("force preparation erased incomplete scope authority")
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func privacyFreshSubsetMutation(mutation m.CanonicalMutation) m.CanonicalMutation {
	fresh := mutation
	fresh.ExpectedScopeVersion = 1
	fresh.Operation.OperationID = "fresh"
	fresh.Operation.Fingerprint = "fresh"

	fresh.Sources = append([]m.CanonicalSourceRecord(nil), mutation.Sources...)
	fresh.Sources[0].SourceID = "fresh-source"

	fresh.Messages = append([]m.MessageRecord(nil), mutation.Messages...)
	for i := range fresh.Messages {
		fresh.Messages[i].SourceID = "fresh-source"
		fresh.Messages[i].MessageID = "fresh-" + strconv.Itoa(i)
	}

	fresh.Revisions = append([]m.MemoryRevision(nil), mutation.Revisions...)
	fresh.Revisions[0].RevisionID = "fresh-revision"
	fresh.Revisions[0].Evidence = append([]m.EvidenceRef(nil), fresh.Revisions[0].Evidence...)
	fresh.Revisions[0].Evidence[0].SourceID = "fresh-source"
	fresh.Revisions[0].Evidence[0].MessageID = fresh.Messages[0].MessageID

	fresh.Heads = []m.ItemHead{{ItemID: fresh.Items[0].ItemID, RevisionID: "fresh-revision"}}

	return fresh
}
