package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baldaworks/boyan"
	"github.com/baldaworks/boyan/app"
	bleveindex "github.com/baldaworks/boyan/index/bleve"
	badgerstore "github.com/baldaworks/boyan/store/badger"
)

var errStopAfterImportProgress = errors.New("stop after import progress")

func TestRunMigrateBaldaMigratesAndRebuildsProjection(t *testing.T) {
	ctx := context.Background()
	sourceDir := filepath.Join(t.TempDir(), "balda-session-memory.badger")
	sourceStore, err := badgerstore.OpenBadgerSessionMemoryStore(sourceDir)
	if err != nil {
		t.Fatalf("OpenBadgerSessionMemoryStore(source) error = %v", err)
	}
	scope := boyan.Scope{Key: "canonical:migrate-search", Kind: boyan.ScopeKindPersonal}
	writeMigrationMutation(t, sourceStore, scope, 0, "alpha", "migrated memory text")
	if err := sourceStore.Close(); err != nil {
		t.Fatalf("Close(source) error = %v", err)
	}

	stateDir := filepath.Join(t.TempDir(), "boyan-state")
	var stdout bytes.Buffer
	if err := run([]string{"migrate", "balda", "--source", sourceDir, "--state-dir", stateDir}, &stdout, ioDiscard{}); err != nil {
		t.Fatalf("run(migrate) error = %v", err)
	}
	if err := run([]string{"migrate", "balda", "--source", sourceDir, "--state-dir", stateDir}, &stdout, ioDiscard{}); err != nil {
		t.Fatalf("run(migrate rerun) error = %v", err)
	}
	receipt, err := loadBaldaMigrationReceipt(filepath.Join(stateDir, baldaMigrationDirName, baldaMigrationReceiptName))
	if err != nil {
		t.Fatalf("loadBaldaMigrationReceipt() error = %v", err)
	}
	if receipt == nil || receipt.Phase != migrationPhaseComplete || receipt.RecordCount == 0 || receipt.CompletedAt == nil {
		t.Fatalf("receipt = %#v", receipt)
	}
	targetStore, err := badgerstore.OpenBadgerSessionMemoryStore(filepath.Join(stateDir, boyanCanonicalDirName))
	if err != nil {
		t.Fatalf("OpenBadgerSessionMemoryStore(target) error = %v", err)
	}
	defer func() { _ = targetStore.Close() }()
	if err := targetStore.VerifyCanonicalIntegrity(ctx); err != nil {
		t.Fatalf("VerifyCanonicalIntegrity() error = %v", err)
	}
	reader, err := badgerstore.NewCanonicalReader(targetStore)
	if err != nil {
		t.Fatalf("NewCanonicalReader() error = %v", err)
	}
	projection, err := bleveindex.Open(filepath.Join(stateDir, boyanProjectionDirName))
	if err != nil {
		t.Fatalf("bleveindex.Open() error = %v", err)
	}
	defer func() { _ = projection.Close() }()
	recall, err := app.NewRecallService(reader, projection)
	if err != nil {
		t.Fatalf("NewRecallService() error = %v", err)
	}
	response, err := recall.Search(ctx, boyan.RecallRequest{
		SchemaVersion: boyan.RecallSchemaVersionV1,
		Scope:         scope,
		Query:         "migrated",
		Limit:         1,
	})
	if err != nil {
		t.Fatalf("RecallService.Search() error = %v", err)
	}
	if len(response.Results) != 1 || !strings.Contains(response.Results[0].Text, "migrated memory text") {
		t.Fatalf("RecallService.Search() = %#v", response.Results)
	}
}

func TestBaldaMigratorResumesPartialImport(t *testing.T) {
	ctx := context.Background()
	sourceDir := filepath.Join(t.TempDir(), "balda-session-memory.badger")
	sourceStore, err := badgerstore.OpenBadgerSessionMemoryStore(sourceDir)
	if err != nil {
		t.Fatalf("OpenBadgerSessionMemoryStore(source) error = %v", err)
	}
	scope := boyan.Scope{Key: "canonical:migrate-resume", Kind: boyan.ScopeKindPersonal}
	for index := 0; index < 40; index++ {
		writeMigrationMutation(t, sourceStore, scope, uint64(index), fmt.Sprintf("resume-%d", index), fmt.Sprintf("resume text %d", index))
	}
	if err := sourceStore.Close(); err != nil {
		t.Fatalf("Close(source) error = %v", err)
	}

	stateDir := filepath.Join(t.TempDir(), "boyan-state")
	migrator := baldaMigrator{
		now: func() time.Time { return time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC) },
		onImportProgress: func(imported uint64) error {
			if imported > 0 {
				return errStopAfterImportProgress
			}
			return nil
		},
	}
	err = migrator.run(ctx, sourceDir, stateDir)
	if !errors.Is(err, errStopAfterImportProgress) {
		t.Fatalf("migrator.run(first) error = %v, want %v", err, errStopAfterImportProgress)
	}
	receipt, err := loadBaldaMigrationReceipt(filepath.Join(stateDir, baldaMigrationDirName, baldaMigrationReceiptName))
	if err != nil {
		t.Fatalf("loadBaldaMigrationReceipt() error = %v", err)
	}
	if receipt == nil || receipt.Phase != migrationPhaseImporting || receipt.ImportedRecords == 0 {
		t.Fatalf("receipt after interrupted import = %#v", receipt)
	}
	if err := (baldaMigrator{now: func() time.Time { return time.Date(2026, time.August, 9, 12, 1, 0, 0, time.UTC) }}).run(ctx, sourceDir, stateDir); err != nil {
		t.Fatalf("migrator.run(resume) error = %v", err)
	}
	targetStore, err := badgerstore.OpenBadgerSessionMemoryStore(filepath.Join(stateDir, boyanCanonicalDirName))
	if err != nil {
		t.Fatalf("OpenBadgerSessionMemoryStore(target) error = %v", err)
	}
	defer func() { _ = targetStore.Close() }()
	state, err := targetStore.LoadScopeState(ctx, scope)
	if err != nil {
		t.Fatalf("LoadScopeState() error = %v", err)
	}
	if state.Version != 40 || state.ChangeSeq != 40 {
		t.Fatalf("LoadScopeState() = %#v, want version/change_seq 40", state)
	}
}

func TestBaldaMigratorFailsClosedOnCorruptExportFooter(t *testing.T) {
	ctx := context.Background()
	sourceDir := filepath.Join(t.TempDir(), "balda-session-memory.badger")
	sourceStore, err := badgerstore.OpenBadgerSessionMemoryStore(sourceDir)
	if err != nil {
		t.Fatalf("OpenBadgerSessionMemoryStore(source) error = %v", err)
	}
	scope := boyan.Scope{Key: "canonical:migrate-corrupt", Kind: boyan.ScopeKindPersonal}
	writeMigrationMutation(t, sourceStore, scope, 0, "corrupt", "corrupt footer test")
	if err := sourceStore.Close(); err != nil {
		t.Fatalf("Close(source) error = %v", err)
	}

	stateDir := filepath.Join(t.TempDir(), "boyan-state")
	migrator := baldaMigrator{
		now: func() time.Time { return time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC) },
		onImportProgress: func(imported uint64) error {
			if imported > 0 {
				return errStopAfterImportProgress
			}
			return nil
		},
	}
	err = migrator.run(ctx, sourceDir, stateDir)
	if !errors.Is(err, errStopAfterImportProgress) {
		t.Fatalf("migrator.run(first) error = %v, want %v", err, errStopAfterImportProgress)
	}
	exportPath := filepath.Join(stateDir, baldaMigrationDirName, baldaMigrationExportName)
	data, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("ReadFile(exportPath) error = %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	var footer map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &footer); err != nil {
		t.Fatalf("json.Unmarshal(footer) error = %v", err)
	}
	footer["record_count"] = float64(footer["record_count"].(float64) + 1)
	encodedFooter, err := json.Marshal(footer)
	if err != nil {
		t.Fatalf("json.Marshal(footer) error = %v", err)
	}
	lines[len(lines)-1] = string(encodedFooter)
	if err := os.WriteFile(exportPath, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(exportPath) error = %v", err)
	}
	err = (baldaMigrator{now: func() time.Time { return time.Date(2026, time.August, 9, 12, 1, 0, 0, time.UTC) }}).run(ctx, sourceDir, stateDir)
	if err == nil || !strings.Contains(err.Error(), "footer does not match records") {
		t.Fatalf("migrator.run(corrupt footer) error = %v", err)
	}
}

func TestRunMigrateBaldaRejectsNonEmptyTargetAndSymlinkSource(t *testing.T) {
	ctx := context.Background()
	sourceDir := filepath.Join(t.TempDir(), "balda-session-memory.badger")
	sourceStore, err := badgerstore.OpenBadgerSessionMemoryStore(sourceDir)
	if err != nil {
		t.Fatalf("OpenBadgerSessionMemoryStore(source) error = %v", err)
	}
	if err := sourceStore.Close(); err != nil {
		t.Fatalf("Close(source) error = %v", err)
	}

	stateDir := filepath.Join(t.TempDir(), "boyan-state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(stateDir) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "foreign.txt"), []byte("occupied"), 0o600); err != nil {
		t.Fatalf("WriteFile(foreign.txt) error = %v", err)
	}
	err = (baldaMigrator{}).run(ctx, sourceDir, stateDir)
	if err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("migrator.run(non-empty target) error = %v", err)
	}

	linkDir := filepath.Join(t.TempDir(), "source-link")
	if err := os.Symlink(sourceDir, linkDir); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	err = (baldaMigrator{}).run(ctx, linkDir, filepath.Join(t.TempDir(), "symlink-state"))
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("migrator.run(symlink source) error = %v", err)
	}
}

func TestRunMigrateBaldaRejectsLockedSource(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "balda-session-memory.badger")
	sourceStore, err := badgerstore.OpenBadgerSessionMemoryStore(sourceDir)
	if err != nil {
		t.Fatalf("OpenBadgerSessionMemoryStore(source) error = %v", err)
	}
	defer func() { _ = sourceStore.Close() }()
	err = (baldaMigrator{}).run(context.Background(), sourceDir, filepath.Join(t.TempDir(), "boyan-state"))
	if err == nil || !strings.Contains(err.Error(), "open source canonical state") {
		t.Fatalf("migrator.run(locked source) error = %v", err)
	}
}

func writeMigrationMutation(t *testing.T, store *badgerstore.BadgerSessionMemoryStore, scope boyan.Scope, expectedVersion uint64, suffix, text string) {
	t.Helper()
	sourceID := "source-" + suffix
	messageID := "message-" + suffix
	itemID := "item-" + suffix
	revisionID := "revision-" + suffix
	operationID := "operation-" + suffix
	now := time.Date(2026, time.August, 9, 9, 0, int(expectedVersion)%60, 0, time.UTC)
	messageText := "evidence " + text
	sourcePayload := []byte(`{"source":"` + suffix + `"}`)
	messagePayload := []byte(messageText)
	revisionPayload := []byte(text)
	sourceRef := payloadRefForTest("payload-source-"+suffix, sourcePayload)
	messageRef := payloadRefForTest("payload-message-"+suffix, messagePayload)
	revisionRef := payloadRefForTest("payload-revision-"+suffix, revisionPayload)
	evidence, err := boyan.NewEvidenceRef(sourceID, messageID, boyan.MessageRoleUser, messageText, 0, uint32(len(messageText)), boyan.AssertionModeUser)
	if err != nil {
		t.Fatalf("NewEvidenceRef() error = %v", err)
	}
	mutation := boyan.CanonicalMutation{
		SchemaVersion:        boyan.CanonicalSchemaVersionV1,
		Scope:                scope,
		ExpectedScopeVersion: expectedVersion,
		Operation: boyan.OperationRecord{
			OperationID: operationID,
			Fingerprint: operationID + "-fingerprint",
			CommittedAt: now,
		},
		Sources: []boyan.SourceRecordV2{{
			SourceID:    sourceID,
			Scope:       scope,
			Sensitivity: boyan.SensitivityStandard,
			Retention:   boyan.RetentionClassStandard,
			Payload:     sourceRef,
		}},
		Messages: []boyan.MessageRecord{{
			MessageID: messageID,
			SourceID:  sourceID,
			Role:      boyan.MessageRoleUser,
			Payload:   messageRef,
		}},
		Items: []boyan.MemoryItem{{
			ItemID: itemID,
			Scope:  scope,
			Kind:   boyan.MemoryKindEvent,
		}},
		Revisions: []boyan.MemoryRevision{{
			SchemaVersion: boyan.MemorySchemaVersionV2,
			RevisionID:    revisionID,
			ItemID:        itemID,
			Revision:      1,
			Temporal:      boyan.Temporal{ObservedAt: now},
			Evidence:      []boyan.EvidenceRef{evidence},
			Sensitivity:   boyan.SensitivityStandard,
			Retention:     boyan.RetentionClassStandard,
			Payload:       revisionRef,
		}},
		Heads: []boyan.ItemHead{{
			ItemID:     itemID,
			RevisionID: revisionID,
		}},
		Payloads: []boyan.CanonicalPayload{
			{Ref: sourceRef, Data: sourcePayload},
			{Ref: messageRef, Data: messagePayload},
			{Ref: revisionRef, Data: revisionPayload},
		},
	}
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatalf("ApplyCanonicalMutation(%s) error = %v", suffix, err)
	}
}

func payloadRefForTest(id string, data []byte) boyan.PayloadRef {
	digest := sha256.Sum256(data)
	return boyan.PayloadRef{
		ID:       id,
		Digest:   hex.EncodeToString(digest[:]),
		ByteSize: uint32(len(data)),
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
