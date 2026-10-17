package standaloneruntime //nolint:testpackage // Exercises SQL reconstruction with canonical privacy.

import (
	"bytes"
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalcore"
	"github.com/baldaworks/boyan/pkg/memory"
	canonical "github.com/baldaworks/boyan/store/badger"
)

// A disposable rebuild, canonical replay, or archive restore must never turn a
// denied/erased revision back into text, even with a healthy semantic provider.
//
//nolint:cyclop,funlen // One lifecycle retains receipts and checks all populated erase row families.
func TestSQLiteRuntimeDeniedReplayRestoreAndErase(t *testing.T) {
	cfg := enabledLifecycleConfig(t, &lifecycleEmbeddingProvider{})
	runtime, err := New(cfg)
	requireNoError(t, err, "new: %v")
	t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
	turn := seedSemanticTurn(t, runtime, "private-lifecycle")
	startSQLComponents(t, runtime)
	waitSemanticState(t, runtime, turn.Scope, "hybrid-ready")
	request := memory.RecallRequest{Scope: turn.Scope, Query: "Bishkek", Limit: 10}
	response, err := runtime.Search(t.Context(), request)
	requireNoError(t, err, "ready recall: %v")

	if len(response.Results) != 1 || response.Retrieval == nil || response.Retrieval.Effective != "hybrid" {
		t.Fatalf("missing healthy control: %#v", response)
	}

	operation, err := memory.CanonicalSemanticOperationID(turn.ExportID, memory.LegacyDerivationRef())
	requireNoError(t, err, "operation: %v")
	receipt, found, err := runtime.service.store.LoadCanonicalOperation(t.Context(), turn.Scope, operation)
	requireNoError(t, err, "receipt: %v")

	if !found {
		t.Fatal("missing original receipt")
	}
	// Deliberately leave the actual SQL rows stale: canonical hydration is the
	// text barrier before any projection synchronization/retirement can run.
	err = runtime.service.store.DenySource(t.Context(), turn.Scope, turn.SourceID, time.Now().UTC())
	requireNoError(t, err, "deny: %v")
	assertSQLPrivacyEmpty(t, runtime, request)
	requireNoError(t, runtime.Stop(t.Context()), "stop: %v")

	runtime, err = New(cfg)
	requireNoError(t, err, "reopen: %v")
	startSQLComponents(t, runtime)
	processor, err := canonicalcore.NewCanonicalTurnProcessor(runtime.service.store, restoreStartupExtractor{},
		memory.PolicyRegistry{Version: "policy-v1"})
	requireNoError(t, err, "processor: %v")
	_, err = processor.ProcessTurn(t.Context(), turn, memory.LegacyDerivationRef())
	requireNoError(t, err, "committed replay: %v")
	after, found, err := runtime.service.store.LoadCanonicalOperation(t.Context(), turn.Scope, operation)
	requireNoError(t, err, "replayed receipt: %v")

	if !found || !reflect.DeepEqual(after, receipt) {
		t.Fatal("replay changed canonical receipt")
	}

	assertSQLPrivacyEmpty(t, runtime, request)

	var archive bytes.Buffer
	requireNoError(t, runtime.service.store.ExportCanonicalLogical(t.Context(), &archive), "export denied state: %v")
	requireNoError(t, runtime.Stop(t.Context()), "stop before restore: %v")

	restoredProvider := &lifecycleEmbeddingProvider{}
	restoredConfig := enabledLifecycleConfig(t, restoredProvider)
	restored, err := canonical.OpenCanonicalStore(filepath.Join(restoredConfig.StateDir, "badger"))
	requireNoError(t, err, "restore store: %v")
	requireNoError(t, restored.ImportCanonicalLogical(t.Context(), bytes.NewReader(archive.Bytes())), "restore: %v")
	requireNoError(t, restored.Close(), "close restore: %v")

	runtime, err = New(restoredConfig)
	requireNoError(t, err, "restored runtime: %v")
	startSQLComponents(t, runtime)
	assertSQLPrivacyEmpty(t, runtime, request)

	// Populate the restored database being erased, in the exact target scope.
	// The denied source must stay absent while a separate eligible source gives
	// every physical row-family removal assertion a nonempty starting point.
	beforeBuild := restoredProvider.calls.Load()
	eligible := seedSemanticScopeTurn(t, runtime, turn.Scope, "restored-eligible")
	syncer := semanticSyncer{lexical: runtime.service.projection, manager: runtime.service.semantic.manager}
	requireNoError(t, syncer.Sync(t.Context(), turn.Scope), "sync restored eligible turn: %v")
	waitSemanticState(t, runtime, turn.Scope, "hybrid-ready")

	if restoredProvider.calls.Load() <= beforeBuild {
		t.Fatal("restored eligible turn did not run passage inference")
	}

	state, published, err := runtime.service.index.ActiveState(t.Context(), turn.Scope)
	requireNoError(t, err, "restored publication: %v")

	if !published || state.PublicationEpoch == 0 {
		t.Fatal("restored eligible turn has no positive semantic publication epoch")
	}

	response, err = runtime.Search(t.Context(), request)
	requireNoError(t, err, "restored eligible recall: %v")

	if len(response.Results) != 1 || response.Results[0].Text != eligible.Messages[0].Text ||
		response.Retrieval == nil || response.Retrieval.Effective != "hybrid" {
		t.Fatalf("restored hybrid control missing or denied text resurrected: %#v", response)
	}

	after, found, err = runtime.service.store.LoadCanonicalOperation(t.Context(), turn.Scope, operation)
	requireNoError(t, err, "restored denied receipt: %v")

	if !found || !reflect.DeepEqual(after, receipt) {
		t.Fatal("restoration or eligible ingest changed denied source receipt")
	}

	assertSQLScopeRows(t, runtime, turn.Scope, true)

	command := memory.ForgetScopeCommand{SchemaVersion: memory.DerivedSchemaVersionV1, Scope: turn.Scope,
		RequestID: "sql-privacy-erase", ForgottenAt: time.Now().UTC()}
	first, err := runtime.ForgetScope(t.Context(), command)
	requireNoError(t, err, "erase: %v")
	assertSQLScopeRows(t, runtime, turn.Scope, false)
	assertSQLPrivacyEmpty(t, runtime, request)

	second, err := runtime.ForgetScope(t.Context(), command)
	requireNoError(t, err, "erase replay: %v")

	if !reflect.DeepEqual(first, second) {
		t.Fatal("erase replay changed durable outcome")
	}

	assertSQLPrivacyEmpty(t, runtime, request)
	assertSQLScopeRows(t, runtime, turn.Scope, false)
}

func assertSQLPrivacyEmpty(t *testing.T, runtime *Runtime, request memory.RecallRequest) {
	t.Helper()
	response, err := runtime.Search(t.Context(), request)
	requireNoError(t, err, "privacy recall: %v")

	if len(response.Results) != 0 {
		t.Fatalf("private canonical text escaped: %#v", response)
	}
}

func assertSQLScopeRows(t *testing.T, runtime *Runtime, scope memory.Scope, populated bool) {
	t.Helper()

	db := openRuntimeSQL(t, runtime)
	defer func() { _ = db.Close() }()

	for table, relation := range map[string]string{
		"lexical_record": "lexical_record r JOIN lexical_build b USING(physical_build_id)",
		"lexical_fts": "lexical_fts f JOIN lexical_record r ON r.rowid=f.rowid " +
			"JOIN lexical_build b USING(physical_build_id)",
		"lexical_source": "lexical_source m JOIN lexical_record r ON r.rowid=m.record_rowid " +
			"JOIN lexical_build b USING(physical_build_id)",
		"lexical_conversation": "lexical_conversation m JOIN lexical_record r ON r.rowid=m.record_rowid " +
			"JOIN lexical_build b USING(physical_build_id)",
		"semantic_chunk":    "semantic_chunk c JOIN semantic_build b USING(build_id)",
		"semantic_coverage": "semantic_coverage c JOIN semantic_build b USING(build_id)",
		"semantic_source": "semantic_source m JOIN semantic_chunk c ON c.rowid=m.chunk_rowid " +
			"JOIN semantic_build b USING(build_id)",
		"semantic_conversation": "semantic_conversation m JOIN semantic_chunk c ON c.rowid=m.chunk_rowid " +
			"JOIN semantic_build b USING(build_id)",
	} {
		var count int

		err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM "+relation+
			" WHERE b.scope_kind=? AND b.scope_key=?", string(scope.Kind), scope.Key).Scan(&count)
		requireNoError(t, err, "logical row count: %v")

		if (populated && count == 0) || (!populated && count != 0) {
			t.Fatalf("scope %s populated=%t rows=%d", table, populated, count)
		}
	}
}
