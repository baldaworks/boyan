package standaloneruntime //nolint:testpackage // Exercises actual restart with an interrupted logical restore.

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalcore"
	"github.com/baldaworks/boyan/internal/canonicalsql"
	m "github.com/baldaworks/boyan/pkg/memory"
	b "github.com/baldaworks/boyan/store/archive"
	"github.com/jackc/pgx/v5"
)

func TestStandaloneLoadingRestoreDoesNotAdvanceErase(t *testing.T) {
	forEachSQLRuntimeConfig(t, testStandaloneLoadingRestoreDoesNotAdvanceErase)
}

func testStandaloneLoadingRestoreDoesNotAdvanceErase(t *testing.T, cfg Config) {
	ctx := context.Background()
	full, original := standalonePendingEraseArchive(t, cfg.Canonical.Backend)
	destination := openFixtureSQLStore(t, cfg)
	t.Cleanup(func() { _ = destination.Close() })

	interruptStandaloneRestore(t, destination, full)
	requireNoError(t, destination.Close(), "close: %v")
	assertStandalonePendingRestoreJournal(t, cfg, original)

	runtime, err := New(cfg)
	requireNoError(t, err, "new: %v")
	t.Cleanup(func() { _ = runtime.Stop(context.Background()) })

	if err := runtime.Start(ctx); err == nil {
		t.Fatal("incomplete restore started")
	} else if code, _, ok := m.ClassifyError(err); !ok || code != m.CodeUnavailable {
		t.Fatalf("loading restore did not block startup: %v", err)
	}

	if runtime.Ready() {
		t.Fatal("incomplete restore became ready")
	}

	requireNoError(t, runtime.Stop(ctx), "stop: %v")
	assertStandalonePendingRestoreJournal(t, cfg, original)

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

func interruptStandaloneRestore(t *testing.T, destination runtimeFixtureStore, full []byte) {
	t.Helper()

	ctx := t.Context()
	summary, err := canonicalsql.ScanCanonicalLogicalExport(ctx, bytes.NewReader(full), 1, nil)
	requireNoError(t, err, "scan complete control: %v")

	interrupted := false

	_, err = destination.ImportCanonicalLogicalWithOptions(ctx, bytes.NewReader(full),
		b.CanonicalLogicalImportOptions{BatchSize: 1, Progress: func(progress b.CanonicalLogicalImportProgress) error {
			if progress.ImportedRecords == summary.RecordCount {
				interrupted = true

				return errors.New("interrupt after durable replay before validation")
			}

			return nil
		}})
	if err == nil || !interrupted {
		t.Fatalf("restore did not reach interrupted durable replay: %v", err)
	}
}

func assertStandalonePendingRestoreJournal(t *testing.T, cfg Config, original m.ScopeEraseJournal) {
	t.Helper()

	args := []any{[]byte(original.Scope.Key), []byte(original.Scope.Kind), []byte(original.OperationID)}

	var encoded []byte

	if cfg.Canonical.Backend == "postgres" {
		conn, err := pgx.Connect(t.Context(), cfg.Canonical.Postgres.DSN)
		requireNoError(t, err, "pending journal connection: %v")

		defer conn.Close(context.Background())

		err = conn.QueryRow(t.Context(), "SELECT envelope FROM boyan_canonical.erase_journal "+
			"WHERE scope_key=$1 AND scope_kind=$2 AND id=$3", args...).Scan(&encoded)
		requireNoError(t, err, "pending restore journal: %v")
	} else {
		db, _ := openRuntimeCanonicalSQL(t, cfg)
		err := db.QueryRowContext(t.Context(), "SELECT envelope FROM erase_journal "+
			"WHERE scope_key=$1 AND scope_kind=$2 AND id=$3", args...).Scan(&encoded)
		requireNoError(t, err, "pending restore journal: %v")
	}

	var journal m.ScopeEraseJournal

	requireNoError(t, canonicalsql.Decode(canonicalsql.EraseJournal, encoded, &journal), "decode journal: %v")

	if !reflect.DeepEqual(journal, original) {
		t.Fatalf("loading restore changed pending journal: %+v", journal)
	}
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

func standalonePendingEraseArchive(t *testing.T, backend string) ([]byte, m.ScopeEraseJournal) {
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
	fresh := openFixtureSQLStore(t, sqlRuntimeConfig(t, backend))

	defer fresh.Close()

	requireNoError(t, fresh.ImportCanonicalLogical(ctx, bytes.NewReader(full.Bytes())), "full control: %v")

	return full.Bytes(), original
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
	full, original := standalonePendingEraseArchive(t, cfg.Canonical.Backend)
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
