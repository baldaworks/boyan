package badger //nolint:testpackage // Verifies untrusted restore metadata and durable readiness fencing.

import (
	"bytes"
	"context"
	"io"
	"testing"

	m "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func TestPrivacyIntegrityRejectsMalformedMetadata(t *testing.T) {
	for _, kind := range []string{
		"version", "state", "scope", "source", "payload", "member", "complete-empty", "stage", "scope-flag", "ready",
	} {
		t.Run(kind, func(t *testing.T) {
			store, mutation := privacyFixture(t)
			if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
				t.Fatal(err)
			}

			corruptRestorePrivacy(t, store, mutation, kind)

			if err := store.VerifyCanonicalIntegrity(context.Background()); err == nil {
				t.Fatal("invalid privacy passed integrity")
			}

			var output bytes.Buffer
			if err := store.ExportCanonicalLogical(context.Background(), &output); err == nil {
				t.Fatal("invalid privacy exported")
			}
		})
	}
}

func corruptRestorePrivacy(t *testing.T, store *CanonicalStore, mutation m.CanonicalMutation, kind string) {
	t.Helper()

	err := store.db.Update(func(txn *badger.Txn) error {
		source := mutation.Sources[0]

		record, _, err := loadSourcePrivacy(txn, source)
		if err != nil {
			return err
		}

		switch kind {
		case "scope-flag":
			key, _ := canonicalKey(mutation.Scope, badgerRecordSourcePrivacyScope, "coverage")

			return putCanonicalRecord(txn, key, badgerRecordSourcePrivacyScope,
				sourcePrivacyScope{Version: 99, Scope: mutation.Scope})
		case "ready":
			return putCanonicalRecord(txn, sourcePrivacyReadinessKey(), badgerRecordSourcePrivacyReady,
				sourcePrivacyReadiness{Version: 99, CoverageVersion: 1})
		}

		record = corruptPrivacyAggregate(record, mutation, kind)
		key, _ := canonicalKey(mutation.Scope, badgerRecordSourcePrivacy, source.SourceID)

		return putCanonicalRecord(txn, key, badgerRecordSourcePrivacy, record)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func corruptPrivacyAggregate(record sourcePrivacy, mutation m.CanonicalMutation, kind string) sourcePrivacy {
	switch kind {
	case "version":
		record.Version++
	case "state":
		record.State = "unsupported"
	case "scope":
		record.Scope.Kind = m.ScopeKindGroup
	case "source":
		record.SourceID = "wrong-source"
	case "payload":
		record.Payload = mutation.Revisions[0].Payload
	case "member":
		record.MessageIDs = []string{"missing-member"}
		record.MessageMembershipComplete = true
	case "complete-empty":
		record.MessageIDs = nil
		record.MessageMembershipComplete = true
	case "stage":
		record.StickyUnknown = true
	}

	return record
}

func TestNativeRestoreCanceledCopiedReadinessStaysUnavailableOnReopen(t *testing.T) {
	source, mutation := privacyFixture(t)
	if _, err := source.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	var backup bytes.Buffer
	if _, err := source.WriteCanonicalBackup(context.Background(), &backup, 0); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()

	destination, err := OpenCanonicalStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	stream := &cancelRestoreReader{Reader: bytes.NewReader(backup.Bytes()), cancel: cancel}
	if err := destination.RestoreCanonicalBackup(ctx, stream, 1); err == nil {
		t.Fatal("canceled restore succeeded")
	}

	assertRestoreUnavailable(t, destination, mutation)

	if err := destination.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenCanonicalStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	assertRestoreUnavailable(t, reopened, mutation)

	if err := reopened.PrepareSourcePrivacy(context.Background()); err == nil {
		t.Fatal("partial restore prepared without complete retry")
	}
}

type cancelRestoreReader struct {
	*bytes.Reader

	cancel context.CancelFunc
}

func (r *cancelRestoreReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if r.Reader.Len() == 0 {
		r.cancel()
	}

	return n, err
}

var _ io.Reader = (*cancelRestoreReader)(nil)

func assertRestoreUnavailable(t *testing.T, store *CanonicalStore, mutation m.CanonicalMutation) {
	t.Helper()

	reader, _ := NewCanonicalReader(store)
	response, err := reader.Trace(context.Background(), privacyTraceRequest(mutation))
	assertTracePrivacyCode(t, response, err, m.CodeUnavailable)
	records, err := reader.LoadRecallRecords(context.Background(), mutation.Scope,
		[]string{mutation.Revisions[0].RevisionID})

	code, _, ok := m.ClassifyError(err)
	if !ok || code != m.CodeUnavailable || len(records) != 0 {
		t.Fatalf("fenced recall served partial data: %+v %v", records, err)
	}
}

func TestNativeRestorePreservesPopulatedRestrictivePrivacy(t *testing.T) {
	for _, kind := range []string{"sensitive", "unknown", "scope"} {
		t.Run(kind, func(t *testing.T) {
			source, mutation := privacyFixture(t)
			if _, err := source.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
				t.Fatal(err)
			}

			initial := nativePrivacyBackup(t, source, 0)

			destination, err := OpenCanonicalStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer destination.Close()

			if err := destination.RestoreCanonicalBackup(context.Background(), bytes.NewReader(initial), 1); err != nil {
				t.Fatal(err)
			}

			setRestrictiveRestorePrivacy(t, destination, mutation, kind)

			for range 100 {
				err := source.db.Update(func(txn *badger.Txn) error {
					record, _, err := loadSourcePrivacy(txn, mutation.Sources[0])
					if err != nil {
						return err
					}

					if err := writeSourcePrivacy(txn, record); err != nil {
						return err
					}

					return putPrivacyScope(txn, mutation.Scope, false)
				})
				if err != nil {
					t.Fatal(err)
				}
			}

			latest := nativePrivacyBackup(t, source, 0)
			if err := destination.RestoreCanonicalBackup(context.Background(), bytes.NewReader(latest), 1); err != nil {
				t.Fatal(err)
			}

			reader, _ := NewCanonicalReader(destination)
			response, err := reader.Trace(context.Background(), privacyTraceRequest(mutation))
			assertTracePrivacyCode(t, response, err, m.CodeForgotten)
		})
	}
}

func nativePrivacyBackup(t *testing.T, store *CanonicalStore, since uint64) []byte {
	t.Helper()

	var encoded bytes.Buffer
	if _, err := store.WriteCanonicalBackup(context.Background(), &encoded, since); err != nil {
		t.Fatal(err)
	}

	return encoded.Bytes()
}

func setRestrictiveRestorePrivacy(t *testing.T, store *CanonicalStore, mutation m.CanonicalMutation, kind string) {
	t.Helper()

	err := store.db.Update(func(txn *badger.Txn) error {
		if kind == "scope" {
			return putPrivacyScope(txn, mutation.Scope, true)
		}

		record, _, err := loadSourcePrivacy(txn, mutation.Sources[0])
		if err != nil {
			return err
		}

		record.State = kind
		record.StickyUnknown = kind == "unknown"

		return writeSourcePrivacy(txn, record)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPrivacyIntegrityRejectsIncompleteEmbeddedCapture(t *testing.T) {
	store, mutation := historicalPrivacyFixture(t, "migrated-embedded-only")
	if err := store.PrepareSourcePrivacy(context.Background()); err != nil {
		t.Fatal(err)
	}

	err := store.db.Update(func(txn *badger.Txn) error {
		record, _, err := loadSourcePrivacy(txn, mutation.Sources[0])
		if err != nil {
			return err
		}

		record.MessageIDs = []string{mutation.Messages[0].MessageID}
		record.MessageMembershipComplete = true

		return writeSourcePrivacy(txn, record)
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.VerifyCanonicalIntegrity(context.Background()); err == nil {
		t.Fatal("embedded partial member set passed complete capture")
	}
}
