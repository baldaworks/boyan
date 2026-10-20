//go:build integration

package storefixture_test

import (
	"reflect"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func ingestRecord(sequence uint64) memory.IngestSequenceRecord {
	scope := memory.Scope{Key: "test:ingest\x00:scope", Kind: memory.ScopeKindPersonal}

	return memory.IngestSequenceRecord{SchemaVersion: memory.IngestSequenceSchemaVersionV1,
		ProducerID: "producer", Scope: scope, Sequence: sequence, Fingerprint: "fingerprint",
		Outcome: memory.IngestOutcome{SchemaVersion: memory.IngestOutcomeSchemaVersionV1,
			Kind: memory.IngestKindTurn, Disposition: memory.IngestDispositionAccepted,
			Projection: memory.ProjectionStatusCurrent, ProducerID: "producer", Scope: scope,
			ScopeSequence: sequence, Fingerprint: "fingerprint", ExportID: "boyan:v1:turn:export",
			OperationIDs: []string{"operation"}, ScopeVersion: 1, ScopeChangeSeq: 1}}
}

func TestPublicIngestSequencePersistence(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, fixture *storefixture.Fixture) {
		store, ok := fixture.Store.(memory.IngestSequenceStore)
		if !ok {
			t.Fatal("SQL owner does not implement durable ingest sequence store")
		}

		record := ingestRecord(1)
		if err := store.SaveIngestSequence(t.Context(), record); err != nil {
			t.Fatal(err)
		}

		assertFirstIngestCursor(t, store, record)

		if err := store.SaveIngestSequence(t.Context(), record); err != nil {
			t.Fatal(err)
		}

		assertFirstIngestCursor(t, store, record)

		if err := fixture.Store.Close(); err != nil {
			t.Fatal(err)
		}

		reopened, err := fixture.Open(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = reopened.Close() })

		store = reopened.(memory.IngestSequenceStore)
		assertFirstIngestCursor(t, store, record)

		if err := store.SaveIngestSequence(t.Context(), record); err != nil {
			t.Fatal(err)
		}

		assertIngestPersistence(t, store, record)
	})
}

func assertFirstIngestCursor(t *testing.T, store memory.IngestSequenceStore, record memory.IngestSequenceRecord) {
	t.Helper()

	cursor, found, err := store.LoadLatestIngestSequence(t.Context(), record.ProducerID, record.Scope)
	if err != nil || !found || cursor.Latest != 1 {
		t.Fatalf("first cursor=%+v found=%t err=%v", cursor, found, err)
	}
}

func assertIngestPersistence(t *testing.T, store memory.IngestSequenceStore, record memory.IngestSequenceRecord) {
	t.Helper()

	got, found, err := store.LoadIngestSequence(t.Context(), record.ProducerID, record.Scope, 1)
	if err != nil || !found || !reflect.DeepEqual(got, record) {
		t.Fatalf("receipt=%+v found=%t err=%v", got, found, err)
	}

	assertFirstIngestCursor(t, store, record)

	if err := store.SaveIngestSequence(t.Context(), ingestRecord(2)); err != nil {
		t.Fatal(err)
	}

	cursor, found, err := store.LoadLatestIngestSequence(t.Context(), record.ProducerID, record.Scope)
	if err != nil || !found || cursor.Latest != 2 {
		t.Fatalf("cursor=%+v found=%t err=%v", cursor, found, err)
	}
}
