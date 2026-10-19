package standaloneruntime //nolint:testpackage // Exercises actual restart with an interrupted logical restore.

import (
	"bytes"
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalcore"
	m "github.com/baldaworks/boyan/pkg/memory"
	b "github.com/baldaworks/boyan/store/archive"
)

func TestStandaloneLoadingRestoreDoesNotAdvanceErase(t *testing.T) {
	forEachSQLRuntimeConfig(t, testStandaloneLoadingRestoreDoesNotAdvanceErase)
}

func testStandaloneLoadingRestoreDoesNotAdvanceErase(t *testing.T, cfg Config) {
	ctx := context.Background()
	full, prefix, original := standalonePendingEraseArchive(t, cfg.Canonical.Backend)
	destination := openFixtureSQLStore(t, cfg)
	requireNoError(t, destination.ImportCanonicalLogical(ctx, bytes.NewReader(full)), "baseline pending journal: %v")

	_, err := destination.ImportCanonicalLogicalWithOptions(ctx, bytes.NewReader(prefix),
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

	destination = openFixtureSQLStore(t, cfg)
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

func standalonePendingEraseArchive(t *testing.T, backend string) ([]byte, []byte, m.ScopeEraseJournal) {
	t.Helper()

	ctx := context.Background()
	source := openFixtureSQLStore(t, sqlRuntimeConfig(t, backend))
	requireNoError(t, source.PrepareSourcePrivacy(t.Context()), "prepare source: %v")

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
	fresh := openFixtureSQLStore(t, sqlRuntimeConfig(t, backend))

	defer fresh.Close()

	requireNoError(t, fresh.ImportCanonicalLogical(ctx, bytes.NewReader(fullBytes)), "full control: %v")

	return fullBytes, prefix, original
}

func reorderStandaloneEraseArchive(t *testing.T, encoded []byte) ([]byte, []byte) {
	t.Helper()
	// SQL archives require stamp order and validate the whole stream before
	// replay. Retain its exact native order and interrupt before the footer.
	trimmed := bytes.TrimSuffix(encoded, []byte{'\n'})
	end := bytes.LastIndexByte(trimmed, '\n') + 1

	prefix := append([]byte(nil), encoded[:end]...)
	if end == 0 || !bytes.HasPrefix(encoded, prefix) {
		t.Fatal("interrupted stream is not exact prefix")
	}

	return encoded, prefix
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
	forEachSQLRuntimeConfig(t, testStandaloneValidatedRestoreResumesBeforeErase)
}

func testStandaloneValidatedRestoreResumesBeforeErase(t *testing.T, cfg Config) {
	ctx := context.Background()
	full, _, original := standalonePendingEraseArchive(t, cfg.Canonical.Backend)
	destination := openFixtureSQLStore(t, cfg)

	removeFault := failRuntimePrivacyPreparation(t, cfg)
	if err := destination.ImportCanonicalLogical(ctx, bytes.NewReader(full)); err == nil {
		t.Fatal("validated preparation fault ignored")
	}

	removeFault()
	requireNoError(t, destination.Close(), "close: %v")

	runtime, err := New(cfg)
	requireNoError(t, err, "new: %v")

	defer runtime.Stop(ctx)

	requireNoError(t, runtime.Start(ctx), "validated startup: %v")
	assertStandaloneRecoveredErase(t, runtime, original)
}
