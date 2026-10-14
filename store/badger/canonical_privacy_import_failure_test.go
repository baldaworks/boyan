package badger //nolint:testpackage // Constructs actual untrusted logical/native streams with valid envelope checksums.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	m "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func TestPrivacyMalformedNativeAndLogicalImportsStayUnavailable(t *testing.T) {
	for _, logical := range []bool{false, true} {
		for _, kind := range []string{
			"version", "state", "scope", "source", "payload", "member", "complete-empty", "stage", "scope-flag", "ready",
		} {
			t.Run(restoreCaseName(logical, kind), func(t *testing.T) {
				source, mutation := privacyFixture(t)
				if _, err := source.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
					t.Fatal(err)
				}

				corruptRestorePrivacy(t, source, mutation, kind)
				encoded := uncheckedPrivacyRestoreBytes(t, source, logical)

				destination := privacyRestoreDestination(t)
				if err := applyPrivacyRestore(destination, logical, encoded); err == nil {
					t.Fatal("malformed imported metadata succeeded")
				}

				assertRestoreUnavailable(t, destination, mutation)

				if err := destination.PrepareSourcePrivacy(context.Background()); err == nil {
					t.Fatal("malformed incomplete import prepared")
				}

				assertNoNativePrivacySpool(t, destination)
			})
		}
	}
}

func uncheckedPrivacyRestoreBytes(t *testing.T, source *CanonicalStore, logical bool) []byte {
	t.Helper()

	if !logical {
		return nativePrivacyBackup(t, source, 0)
	}

	var stream bytes.Buffer
	requireTraceNoError(t, writeCanonicalLogicalJSON(&stream, CanonicalLogicalExportHeader{
		SchemaVersion: canonicalLogicalExportSchema, Kind: canonicalLogicalHeaderKind}), "header: %v")

	hash := sha256.New()
	count := uint64(0)
	err := source.db.View(func(txn *badger.Txn) error {
		iterator := txn.NewIterator(badger.DefaultIteratorOptions)
		defer iterator.Close()

		for iterator.Rewind(); iterator.Valid(); iterator.Next() {
			item := iterator.Item()

			components, suffix, err := decodeCanonicalLogicalKey(item.Key())
			if err != nil {
				return err
			}

			encoded, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}

			record := CanonicalLogicalExportRecord{Kind: canonicalLogicalRecordKind, RecordType: components[0],
				Components: components, Suffix: suffix, Envelope: encoded}

			line, err := marshalCanonicalLogicalJSON(record)
			if err != nil {
				return err
			}

			line = append(line, '\n')
			stream.Write(line)
			_, _ = hash.Write(line)
			count++
		}

		return nil
	})
	requireTraceNoError(t, err, "unchecked fixture stream: %v")
	requireTraceNoError(t, writeCanonicalLogicalJSON(&stream, CanonicalLogicalExportFooter{
		SchemaVersion: canonicalLogicalExportSchema, Kind: canonicalLogicalFooterKind, RecordCount: count,
		SHA256: hex.EncodeToString(hash.Sum(nil))}), "footer: %v")

	return stream.Bytes()
}

func TestLogicalPrivacyFooterAndCancellationRequireCompleteRetry(t *testing.T) {
	for _, kind := range []string{"footer", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			source, mutation := privacyFixture(t)
			if _, err := source.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
				t.Fatal(err)
			}

			encoded := privacyRestoreBytes(t, source, true)

			stream := encoded
			if kind == "footer" {
				lines := strings.Split(strings.TrimSpace(string(encoded)), "\n")
				stream = []byte(strings.Join(lines[:len(lines)-1], "\n") + "\n")
			}

			dir := t.TempDir()

			destination, err := OpenCanonicalStore(dir)
			if err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			options := CanonicalLogicalImportOptions{BatchSize: 1}
			if kind == "cancel" {
				options.Progress = func(CanonicalLogicalImportProgress) error {
					cancel()

					return nil
				}
			}

			if _, err := destination.ImportCanonicalLogicalWithOptions(ctx, bytes.NewReader(stream), options); err == nil {
				t.Fatal("incomplete logical import succeeded")
			}

			assertRestoreUnavailable(t, destination, mutation)

			reopened := reopenLoadingPrivacyStore(t, destination, dir, mutation)
			defer reopened.Close()

			if err := applyPrivacyRestore(reopened, true, encoded); err != nil {
				t.Fatalf("complete logical retry failed: %v", err)
			}

			assertPrivacyRestoredOutcome(t, reopened, mutation, "native-standard")
		})
	}
}

func TestNativePrivacyRejectsOriginalBeforeRestrictiveMerge(t *testing.T) {
	for _, kind := range []string{"stage", "different-payload"} {
		t.Run(kind, func(t *testing.T) {
			source, mutation := privacyFixture(t)
			if _, err := source.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
				t.Fatal(err)
			}

			destination := privacyRestoreDestination(t)
			if err := applyPrivacyRestore(destination, false, nativePrivacyBackup(t, source, 0)); err != nil {
				t.Fatal(err)
			}

			setRestrictiveRestorePrivacy(t, destination, mutation, "sensitive")

			if kind == "stage" {
				corruptRestorePrivacy(t, source, mutation, kind)
			} else {
				replaceIncomingPrivacyPayload(t, source, mutation)
			}

			if err := applyPrivacyRestore(destination, false, nativePrivacyBackup(t, source, 0)); err == nil {
				t.Fatal("restrictive merge masked invalid original/binding")
			}

			assertRestoreUnavailable(t, destination, mutation)
		})
	}
}

func replaceIncomingPrivacyPayload(t *testing.T, store *CanonicalStore, mutation m.CanonicalMutation) {
	t.Helper()

	err := store.db.Update(func(txn *badger.Txn) error {
		source := mutation.Sources[0]

		record, _, err := loadSourcePrivacy(txn, source)
		if err != nil {
			return err
		}

		data := append(append([]byte(nil), mutation.Payloads[0].Data...), ' ')
		source.Payload = payloadRef("different-source-payload", data)

		record.Payload = source.Payload
		if err := putCanonicalMutationPayloads(txn, []m.CanonicalPayload{{Ref: source.Payload, Data: data}}); err != nil {
			return err
		}

		key, _ := canonicalKey(mutation.Scope, badgerRecordSource, source.SourceID)
		if err := putCanonicalRecord(txn, key, badgerRecordSource, source); err != nil {
			return err
		}

		return writeSourcePrivacy(txn, record)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func reopenLoadingPrivacyStore(
	t *testing.T, destination *CanonicalStore, dir string, mutation m.CanonicalMutation,
) *CanonicalStore {
	t.Helper()

	if err := destination.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenCanonicalStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	assertRestoreUnavailable(t, reopened, mutation)

	if err := reopened.PrepareSourcePrivacy(context.Background()); err == nil {
		t.Fatal("incomplete logical import prepared")
	}

	return reopened
}
