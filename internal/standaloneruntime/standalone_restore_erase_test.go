package standaloneruntime //nolint:testpackage // Exercises actual restart with an interrupted logical restore.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalcore"
	m "github.com/baldaworks/boyan/pkg/memory"
	b "github.com/baldaworks/boyan/store/badger"
)

func TestStandaloneLoadingRestoreDoesNotAdvanceErase(t *testing.T) {
	ctx := context.Background()
	full, prefix, original := standalonePendingEraseArchive(t)
	cfg := testStandaloneConfig(t)
	dir := filepath.Join(cfg.StateDir, "badger")
	destination, err := b.OpenCanonicalStore(dir)
	requireNoError(t, err, "open: %v")

	_, err = destination.ImportCanonicalLogicalWithOptions(ctx, bytes.NewReader(prefix),
		b.CanonicalLogicalImportOptions{BatchSize: 1})
	if err == nil {
		t.Fatal("partial import succeeded")
	}

	requireNoError(t, destination.Close(), "close: %v")

	runtime, err := New(cfg)
	requireNoError(t, err, "new: %v")

	if err := runtime.Start(ctx); err == nil {
		t.Fatal("incomplete restore started")
	}

	if runtime.Ready() {
		t.Fatal("incomplete restore became ready")
	}

	requireNoError(t, runtime.Stop(ctx), "stop: %v")

	destination, err = b.OpenCanonicalStore(dir)
	requireNoError(t, err, "reopen: %v")
	// Complete retry must preserve the imported prepared journal, then permit normal recovery.
	requireNoError(t, destination.ImportCanonicalLogical(ctx, bytes.NewReader(full)), "full retry after startup: %v")
	journal, found, err := destination.LoadScopeErase(ctx, original.Scope, original.OperationID)
	requireNoError(t, err, "journal: %v")

	if !found || !reflect.DeepEqual(journal, original) {
		t.Fatalf("startup mutated pending journal: %+v", journal)
	}

	requireNoError(t, destination.Close(), "close restored: %v")

	runtime, err = New(cfg)
	requireNoError(t, err, "new restored: %v")

	defer runtime.Stop(ctx)

	requireNoError(t, runtime.Start(ctx), "start restored: %v")
	assertStandaloneRecoveredErase(t, runtime, original)
}

func assertStandaloneRecoveredErase(t *testing.T, runtime *Runtime, original m.ScopeEraseJournal) {
	t.Helper()

	ctx := context.Background()
	journal, found, err := runtime.service.store.LoadScopeErase(ctx, original.Scope, original.OperationID)
	requireNoError(t, err, "recovered: %v")

	if !runtime.Ready() || !found || journal.Stage != m.ScopeEraseComplete {
		t.Fatalf("restored startup not recovered: %+v", journal)
	}

	if journal.SourceCount == 0 || journal.RevisionCount == 0 {
		t.Fatalf("restored startup not recovered: %+v", journal)
	}
}

func standalonePendingEraseArchive(t *testing.T) ([]byte, []byte, m.ScopeEraseJournal) {
	t.Helper()

	ctx := context.Background()
	source, err := b.OpenCanonicalStore(t.TempDir())
	requireNoError(t, err, "source: %v")

	defer source.Close()

	scope := m.Scope{Key: "test:restore-startup", Kind: m.ScopeKindPersonal}
	processor, err := canonicalcore.NewCanonicalTurnProcessor(source, restoreStartupExtractor{},
		m.PolicyRegistry{Version: "policy-v1"})
	requireNoError(t, err, "processor: %v")
	turn, err := m.NewTurn(scope, m.ConversationRef{ConversationID: "restore", AgentConversationID: "restore"},
		"turn", time.Now().UTC(), "restore memory", "OK")
	requireNoError(t, err, "turn: %v")
	_, err = processor.ProcessTurn(ctx, turn, m.LegacyDerivationRef())
	requireNoError(t, err, "process: %v")
	state, err := source.LoadScopeState(ctx, scope)
	requireNoError(t, err, "scope: %v")
	original, err := source.BeginScopeErase(ctx, m.ScopeEraseRequest{
		Scope: scope, OperationID: "restore-startup",
		Fingerprint: "restore-startup", ForgottenAt: time.Now().UTC(), ExpectedScopeVersion: state.Version,
	})
	requireNoError(t, err, "begin: %v")

	var full bytes.Buffer
	requireNoError(t, source.ExportCanonicalLogical(ctx, &full), "export: %v")
	fullBytes, prefix := reorderStandaloneEraseArchive(t, full.Bytes())
	fresh, err := b.OpenCanonicalStore(t.TempDir())
	requireNoError(t, err, "control: %v")

	defer fresh.Close()

	requireNoError(t, fresh.ImportCanonicalLogical(ctx, bytes.NewReader(fullBytes)), "full control: %v")

	return fullBytes, prefix, original
}

func reorderStandaloneEraseArchive(t *testing.T, encoded []byte) ([]byte, []byte) {
	t.Helper()

	var full bytes.Buffer

	lines := bytes.Split(bytes.TrimSpace(encoded), []byte{'\n'})

	var first, rest [][]byte

	for _, line := range lines[1 : len(lines)-1] {
		var record b.CanonicalLogicalExportRecord
		requireNoError(t, json.Unmarshal(line, &record), "record: %v")

		switch record.RecordType {
		case "erase-journal", "erase-fence", "erase-guard", "scope":
			first = append(first, line)
		default:
			rest = append(rest, line)
		}
	}

	body := append(bytes.Join(append(first, rest...), []byte{'\n'}), '\n')
	digest := sha256.Sum256(body)
	footer, err := json.Marshal(b.CanonicalLogicalExportFooter{SchemaVersion: "boyan-logical-export/v1", Kind: "footer",
		RecordCount: uint64(len(first) + len(rest)), SHA256: hex.EncodeToString(digest[:])})
	requireNoError(t, err, "footer: %v")
	full.Reset()
	full.Write(lines[0])
	full.WriteByte('\n')
	full.Write(body)
	full.Write(footer)
	full.WriteByte('\n')

	prefix := append(append(append([]byte{}, lines[0]...), '\n'), append(bytes.Join(first, []byte{'\n'}), '\n')...)
	if !bytes.HasPrefix(full.Bytes(), prefix) {
		t.Fatal("interrupted stream is not exact prefix")
	}

	return full.Bytes(), prefix
}

type restoreStartupExtractor struct{}

func (restoreStartupExtractor) ExtractCanonicalSemantics(
	_ context.Context, request m.CanonicalExtractionRequest,
) ([]m.SemanticCandidate, error) {
	msg := request.Turn.Messages[0]

	evidence, err := m.NewEvidenceRef(request.Turn.SourceID, msg.MessageID, msg.Role, msg.Text, 0,
		uint32(len(msg.Text)), m.AssertionModeUser)
	if err != nil {
		return nil, err
	}

	return []m.SemanticCandidate{{Kind: m.MemoryKindState, Subject: msg.Text, Predicate: "memory",
		Memory: m.MemoryCandidate{Kind: m.MemoryKindState, Statement: msg.Text,
			Temporal: m.Temporal{ObservedAt: request.Turn.CompletedAt},
			Evidence: []m.EvidenceRef{evidence}, Sensitivity: m.SensitivityStandard, Retention: m.RetentionClassStandard}}}, nil
}

func TestStandaloneValidatedRestoreResumesBeforeErase(t *testing.T) {
	ctx := context.Background()
	full, _, original := standalonePendingEraseArchive(t)
	cfg := testStandaloneConfig(t)
	dir := filepath.Join(cfg.StateDir, "badger")
	destination, err := b.OpenCanonicalStore(dir)
	requireNoError(t, err, "open: %v")
	requireNoError(t, destination.ImportCanonicalLogical(ctx, bytes.NewReader(full)), "restore: %v")
	requireNoError(t, destination.Close(), "close: %v")
	// Simulate the durable phase after archive validation and before readiness publication.
	err = os.WriteFile(filepath.Join(dir, ".boyan-restore-privacy"), []byte("v1 validated\n"), 0o600)
	requireNoError(t, err, "fence: %v")

	runtime, err := New(cfg)
	requireNoError(t, err, "new: %v")

	defer runtime.Stop(ctx)

	requireNoError(t, runtime.Start(ctx), "validated startup: %v")
	assertStandaloneRecoveredErase(t, runtime, original)
}
