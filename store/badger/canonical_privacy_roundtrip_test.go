package badger //nolint:testpackage // Exercises actual restore streams and authoritative classification.

import (
	"bytes"
	"context"
	"errors"
	"testing"

	m "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func TestPrivacyNativeAndLogicalRoundTrips(t *testing.T) {
	for _, logical := range []bool{false, true} {
		for _, kind := range []string{
			"native-standard", "migrated-embedded-only", "mixed-same", "mixed-separate",
			"opaque-source", "incomplete-evidence", "missing", "forged-standard",
		} {
			t.Run(restoreCaseName(logical, kind), func(t *testing.T) {
				source, mutation := privacyFixture(t)
				setupPrivacyRoundtrip(t, source, &mutation, kind)
				encoded := privacyRestoreBytes(t, source, logical)

				destination := privacyRestoreDestination(t)
				if err := applyPrivacyRestore(destination, logical, encoded); err != nil {
					t.Fatal(err)
				}

				assertPrivacyRestoredOutcome(t, destination, mutation, kind)

				if logical && kind == "native-standard" {
					if err := applyPrivacyRestore(destination, true, encoded); err != nil {
						t.Fatalf("same logical stream not idempotent: %v", err)
					}
				}

				if kind == "native-standard" {
					assertPostRestoreSensitivityAtomic(t, destination, mutation)
				}
			})
		}
	}
}

func restoreCaseName(logical bool, kind string) string {
	if logical {
		return "logical/" + kind
	}

	return "native/" + kind
}

func setupPrivacyRoundtrip(t *testing.T, source *CanonicalStore, mutation *m.CanonicalMutation, kind string) {
	t.Helper()

	configure := kind
	if kind == "forged-standard" {
		configure = "mixed-same"
	}

	if kind == "missing" {
		configure = "native-standard"
	}

	configurePrivacyHistory(t, mutation, configure)

	if _, err := source.ApplyCanonicalMutation(context.Background(), *mutation); err != nil {
		t.Fatal(err)
	}

	if kind == "migrated-embedded-only" {
		makePrivacyHistoryOld(t, source, *mutation)
		dropHistoricalMessageRecords(t, source, *mutation)
	}

	if kind == "missing" {
		makePrivacyHistoryOld(t, source, *mutation)
	}

	if kind == "forged-standard" {
		err := source.db.Update(func(txn *badger.Txn) error {
			record, _, err := loadSourcePrivacy(txn, mutation.Sources[0])
			if err != nil {
				return err
			}

			record.State = sourcePrivacyStandard
			record.StickyUnknown = false

			return writeSourcePrivacy(txn, record)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func privacyRestoreBytes(t *testing.T, source *CanonicalStore, logical bool) []byte {
	t.Helper()

	var output bytes.Buffer
	if logical {
		if err := source.ExportCanonicalLogical(context.Background(), &output); err != nil {
			t.Fatal(err)
		}
	} else {
		if _, err := source.WriteCanonicalBackup(context.Background(), &output, 0); err != nil {
			t.Fatal(err)
		}
	}

	return output.Bytes()
}

func privacyRestoreDestination(t *testing.T) *CanonicalStore {
	t.Helper()

	store, err := OpenCanonicalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	return store
}

func applyPrivacyRestore(store *CanonicalStore, logical bool, encoded []byte) error {
	if logical {
		return store.ImportCanonicalLogical(context.Background(), bytes.NewReader(encoded))
	}

	return store.RestoreCanonicalBackup(context.Background(), bytes.NewReader(encoded), 1)
}

func assertPrivacyRestoredOutcome(t *testing.T, store *CanonicalStore, mutation m.CanonicalMutation, kind string) {
	t.Helper()

	reader, _ := NewCanonicalReader(store)

	response, err := reader.Trace(context.Background(), privacyTraceRequest(mutation))
	if kind == "native-standard" || kind == "migrated-embedded-only" || kind == "missing" {
		assertRestoredStandardTrace(t, response, err)
	} else {
		assertTracePrivacyCode(t, response, err, m.CodeForgotten)
	}

	if kind == "mixed-same" || kind == "mixed-separate" || kind == "forged-standard" {
		assertRestoredBenignRecall(t, reader, mutation)
	}
}

func assertRestoredStandardTrace(t *testing.T, response m.TraceResponse, err error) {
	t.Helper()

	if err != nil || len(response.Sources) != 1 || len(response.Revisions) != 1 {
		t.Fatalf("standard restore lost graph: %+v %v", response, err)
	}
}

func assertRestoredBenignRecall(t *testing.T, reader *CanonicalReader, mutation m.CanonicalMutation) {
	t.Helper()

	records, err := reader.LoadRecallRecords(context.Background(), mutation.Scope,
		[]string{mutation.Revisions[0].RevisionID})
	if err != nil || len(records) != 1 || records[0].Text != "protected memory" {
		t.Fatalf("benign recall changed: %+v %v", records, err)
	}
}

func assertPostRestoreSensitivityAtomic(t *testing.T, store *CanonicalStore, mutation m.CanonicalMutation) {
	t.Helper()

	later := lateTraceSensitive(mutation)
	later.Items = append([]m.MemoryItem(nil), mutation.Items...)
	later.Items[0].ItemID = "sensitive-after-restore"
	later.Revisions[0].ItemID = later.Items[0].ItemID
	later.Heads[0].ItemID = later.Items[0].ItemID

	store.beforeCanonicalMutationCommit = func() error { return errors.New("abort late-sensitive") }
	if _, err := store.ApplyCanonicalMutation(context.Background(), later); err == nil {
		t.Fatal("late-sensitive abort ignored")
	}

	if got := privacyState(t, store, mutation.Scope, mutation.Sources[0].SourceID); got != sourcePrivacyStandard {
		t.Fatal("aborted sensitivity published")
	}

	store.beforeCanonicalMutationCommit = nil
	if _, err := store.ApplyCanonicalMutation(context.Background(), later); err != nil {
		t.Fatal(err)
	}

	reader, _ := NewCanonicalReader(store)
	response, err := reader.Trace(context.Background(), privacyTraceRequest(mutation))
	assertTracePrivacyCode(t, response, err, m.CodeForgotten)

	records, err := reader.LoadRecallRecords(context.Background(), mutation.Scope,
		[]string{mutation.Revisions[0].RevisionID})
	if err != nil || len(records) != 1 {
		t.Fatalf("benign recall lost after sensitive derivation: %+v %v", records, err)
	}
}
