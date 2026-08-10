package badger

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func TestCanonicalStorePersistsIngestSequenceReplay(t *testing.T) {
	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "canonical.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	record := testIngestSequenceRecord(1)
	if err := store.SaveIngestSequence(context.Background(), record); err != nil {
		t.Fatalf("SaveIngestSequence() error = %v", err)
	}
	if err := store.SaveIngestSequence(context.Background(), record); err != nil {
		t.Fatalf("SaveIngestSequence(replay) error = %v", err)
	}

	cursor, found, err := store.LoadLatestIngestSequence(context.Background(), record.ProducerID, record.Scope)
	if err != nil || !found {
		t.Fatalf("LoadLatestIngestSequence() = %+v, found=%v, error=%v", cursor, found, err)
	}
	if cursor.Latest != 1 {
		t.Fatalf("cursor latest = %d, want 1", cursor.Latest)
	}

	got, found, err := store.LoadIngestSequence(context.Background(), record.ProducerID, record.Scope, record.Sequence)
	if err != nil || !found {
		t.Fatalf("LoadIngestSequence() = %+v, found=%v, error=%v", got, found, err)
	}
	if !reflect.DeepEqual(got, record) {
		t.Fatalf("stored record = %+v, want %+v", got, record)
	}

	second := testIngestSequenceRecord(2)
	if err := store.SaveIngestSequence(context.Background(), second); err != nil {
		t.Fatalf("SaveIngestSequence(second) error = %v", err)
	}
	cursor, found, err = store.LoadLatestIngestSequence(context.Background(), second.ProducerID, second.Scope)
	if err != nil || !found || cursor.Latest != 2 {
		t.Fatalf("LoadLatestIngestSequence(after second) = %+v, found=%v, error=%v", cursor, found, err)
	}
}

func TestCanonicalStoreRejectsConflictingOrSkippedIngestSequence(t *testing.T) {
	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "canonical.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	first := testIngestSequenceRecord(1)
	if err := store.SaveIngestSequence(context.Background(), first); err != nil {
		t.Fatalf("SaveIngestSequence(first) error = %v", err)
	}

	conflict := testIngestSequenceRecord(1)
	conflict.Fingerprint = "ingest-fingerprint-conflict"
	conflict.Outcome.Fingerprint = conflict.Fingerprint
	if err := store.SaveIngestSequence(context.Background(), conflict); !hasStateCode(err, boyan.CodeConflict) {
		t.Fatalf("SaveIngestSequence(conflict) error = %v, want conflict", err)
	}

	skipped := testIngestSequenceRecord(3)
	if err := store.SaveIngestSequence(context.Background(), skipped); !hasStateCode(err, boyan.CodeConflict) {
		t.Fatalf("SaveIngestSequence(skip) error = %v, want conflict", err)
	}
}

func TestCanonicalStoreRestoresIngestSequenceAfterReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "canonical.badger")
	store, err := OpenCanonicalStore(path)
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	record := testIngestSequenceRecord(1)
	if err := store.SaveIngestSequence(context.Background(), record); err != nil {
		t.Fatalf("SaveIngestSequence() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := OpenCanonicalStore(path)
	if err != nil {
		t.Fatalf("OpenCanonicalStore(reopen) error = %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	cursor, found, err := reopened.LoadLatestIngestSequence(context.Background(), record.ProducerID, record.Scope)
	if err != nil || !found || cursor.Latest != 1 {
		t.Fatalf("LoadLatestIngestSequence(after reopen) = %+v, found=%v, error=%v", cursor, found, err)
	}
}

func testIngestSequenceRecord(sequence uint64) boyan.IngestSequenceRecord {
	scope := boyan.Scope{Key: "telegram:ingest-sequence", Kind: boyan.ScopeKindPersonal}
	fingerprint := "ingest-fingerprint"
	return boyan.IngestSequenceRecord{
		SchemaVersion: boyan.IngestSequenceSchemaVersionV1,
		ProducerID:    "producer-1",
		Scope:         scope,
		Sequence:      sequence,
		Fingerprint:   fingerprint,
		Outcome: boyan.IngestOutcome{
			SchemaVersion:  boyan.IngestOutcomeSchemaVersionV1,
			Kind:           boyan.IngestKindTurn,
			Disposition:    boyan.IngestDispositionAccepted,
			Projection:     boyan.ProjectionStatusCurrent,
			ProducerID:     "producer-1",
			Scope:          scope,
			ScopeSequence:  sequence,
			Fingerprint:    fingerprint,
			ExportID:       "boyan:v1:turn:export-1",
			OperationIDs:   []string{"boyan-memory:v1:operation:1"},
			ScopeVersion:   sequence,
			ScopeChangeSeq: sequence,
		},
	}
}
