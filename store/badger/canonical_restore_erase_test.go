package badger //nolint:testpackage // Verifies loading-fenced erase transactions and validated restart seams.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	m "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func pendingEraseArchive(t *testing.T) ([]byte, []byte, m.ScopeEraseJournal) {
	t.Helper()

	ctx := context.Background()

	source, mutation := privacyFixture(t)
	if _, err := source.ApplyCanonicalMutation(ctx, mutation); err != nil {
		t.Fatal(err)
	}

	state, err := source.LoadScopeState(ctx, mutation.Scope)
	requireEraseTestNoError(t, err)
	journal, err := source.BeginScopeErase(ctx, m.ScopeEraseRequest{
		Scope: mutation.Scope, OperationID: "restore-erase", Fingerprint: "restore-erase",
		ExpectedScopeVersion: state.Version, ForgottenAt: time.Now().UTC(),
	})
	requireEraseTestNoError(t, err)

	var archive bytes.Buffer
	requireEraseTestNoError(t, source.ExportCanonicalLogical(ctx, &archive))
	lines := bytes.Split(bytes.TrimSpace(archive.Bytes()), []byte{'\n'})

	var first, rest [][]byte

	for _, line := range lines[1 : len(lines)-1] {
		var record CanonicalLogicalExportRecord
		requireEraseTestNoError(t, json.Unmarshal(line, &record))

		switch record.RecordType {
		case badgerRecordEraseJournal, badgerRecordEraseFence, badgerRecordEraseGuard, "scope":
			first = append(first, line)
		default:
			rest = append(rest, line)
		}
	}

	if len(first) < 4 {
		t.Fatal("missing recovery records")
	}

	ordered := append(first, rest...)
	body := append(bytes.Join(ordered, []byte{'\n'}), '\n')
	digest := sha256.Sum256(body)
	footer, err := json.Marshal(CanonicalLogicalExportFooter{
		SchemaVersion: canonicalLogicalExportSchema, Kind: canonicalLogicalFooterKind,
		RecordCount: uint64(len(ordered)), SHA256: hex.EncodeToString(digest[:]),
	})
	requireEraseTestNoError(t, err)

	full := append(append(append(append([]byte{}, lines[0]...), '\n'), body...), footer...)
	full = append(full, '\n')

	prefix := append(append(append([]byte{}, lines[0]...), '\n'), append(bytes.Join(first, []byte{'\n'}), '\n')...)
	if !bytes.HasPrefix(full, prefix) {
		t.Fatal("interruption is not an exact prefix")
	}

	return full, prefix, journal
}

func TestLoadingRestoreFencesEraseAndAllowsCompleteRetry(t *testing.T) {
	ctx := context.Background()
	full, prefix, original := pendingEraseArchive(t)

	fresh := privacyRestoreDestination(t)
	if err := fresh.ImportCanonicalLogical(ctx, bytes.NewReader(full)); err != nil {
		t.Fatalf("complete reordered control: %v", err)
	}

	dir := t.TempDir()
	destination, err := OpenCanonicalStore(dir)
	requireEraseTestNoError(t, err)

	_, err = destination.ImportCanonicalLogicalWithOptions(ctx, bytes.NewReader(prefix),
		CanonicalLogicalImportOptions{BatchSize: 1})
	if err == nil {
		t.Fatal("incomplete stream succeeded")
	}

	assertLoadingEraseFence(t, destination, original)

	requireEraseTestNoError(t, destination.Close())
	destination, err = OpenCanonicalStore(dir)
	requireEraseTestNoError(t, err)

	defer destination.Close()

	if err := destination.ImportCanonicalLogical(ctx, bytes.NewReader(prefix)); err == nil {
		t.Fatal("malformed retry succeeded")
	}

	if !destination.restoreBlocked.Load() {
		t.Fatal("malformed retry released fence")
	}

	if err = destination.ImportCanonicalLogical(ctx, bytes.NewReader(full)); err != nil {
		t.Fatalf("complete retry: %v", err)
	}

	assertRestoredErase(t, destination, original)
}

func assertRestoredErase(t *testing.T, destination *CanonicalStore, original m.ScopeEraseJournal) {
	t.Helper()

	ctx := context.Background()

	got, found, err := destination.LoadScopeErase(ctx, original.Scope, original.OperationID)
	if err != nil || !found || got.Stage != original.Stage || got.SourceCount != 0 || got.RevisionCount != 0 {
		t.Fatalf("journal advanced before recovery: %+v %v", got, err)
	}

	for got.Stage != m.ScopeEraseComplete {
		got, err = destination.AdvanceScopeErase(ctx, got.Scope, got.OperationID)
		if err != nil {
			t.Fatal(err)
		}
	}

	if got.SourceCount == 0 || got.RevisionCount == 0 {
		t.Fatalf("recovery missed restored data: %+v", got)
	}
}

func TestEraseRecoveryRestorePreflight(t *testing.T) {
	full, _, _ := pendingEraseArchive(t)
	ctx := context.Background()
	dir := t.TempDir()
	destination, err := OpenCanonicalStore(dir)
	requireEraseTestNoError(t, err)
	// Fail after the validated fence is durable, before readiness is published.
	destination.beforeRestoreDBSync = privacyRestoreSyncFault("sync-validated")
	if err = destination.ImportCanonicalLogical(ctx, bytes.NewReader(full)); err == nil {
		t.Fatal("sync fault ignored")
	}

	if destination.restorePhase != restoreValidated {
		t.Fatal("fixture is not validated")
	}

	requireEraseTestNoError(t, destination.Close())
	destination, err = OpenCanonicalStore(dir)
	requireEraseTestNoError(t, err)

	defer destination.Close()

	requireEraseTestNoError(t, destination.ResumeRestoreBeforeEraseRecovery(ctx))

	if destination.restoreBlocked.Load() {
		t.Fatal("validated recovery did not release fence")
	}
	// A live pending journal must still fence a new import.
	err = destination.ImportCanonicalLogical(ctx, bytes.NewReader(full))
	if code, _, ok := m.ClassifyError(err); !ok || code != m.CodeConflict {
		t.Fatalf("live erase import guard: %v", err)
	}
	// Legacy readiness is deliberately irrelevant to this narrow preflight.
	requireEraseTestNoError(t, destination.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(sourcePrivacyReadinessKey())
	}))

	requireEraseTestNoError(t, destination.ResumeRestoreBeforeEraseRecovery(ctx))

	journals, _, err := destination.ListPendingScopeErases(ctx, nil, 128)
	if err != nil || len(journals) != 1 {
		t.Fatalf("legacy pending erase: %v", err)
	}

	assertRestoredErase(t, destination, journals[0])
	requireEraseTestNoError(t, destination.PrepareSourcePrivacy(ctx))
}

func assertLoadingEraseFence(t *testing.T, destination *CanonicalStore, original m.ScopeEraseJournal) {
	t.Helper()

	ctx := context.Background()
	_, err := destination.AdvanceScopeErase(ctx, original.Scope, original.OperationID)
	requireTurnIdentityCode(t, err, m.CodeUnavailable)
	_, err = destination.CommitCanonicalOperation(ctx, m.CanonicalOperationCommitRequest{
		Scope: original.Scope, OperationID: "blocked-writer", Fingerprint: "blocked-writer", CommittedAt: time.Now().UTC()})
	requireTurnIdentityCode(t, err, m.CodeUnavailable)
	requireTurnIdentityCode(t, destination.ResumeRestoreBeforeEraseRecovery(ctx), m.CodeUnavailable)

	var journal m.ScopeEraseJournal

	requireEraseTestNoError(t, destination.db.View(func(txn *badger.Txn) error {
		loaded, _, loadErr := loadScopeEraseTxn(txn, original.Scope, original.OperationID)
		journal = loaded

		return loadErr
	}))

	if !reflect.DeepEqual(journal, original) {
		t.Fatalf("loading erase or writer changed journal: %+v", journal)
	}
}
