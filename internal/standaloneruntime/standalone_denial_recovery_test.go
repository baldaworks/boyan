//go:build integration

package standaloneruntime //nolint:testpackage // Startup must retire the existing database before readiness.

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/forgetops"
	"github.com/baldaworks/boyan/internal/memoryops"
	"github.com/baldaworks/boyan/pkg/memory"
)

// A completed canonical journal is not returned by pending recovery. Removing
// the all-SQL startup pass leaves its old semantic rows in a disabled reopen.
func TestStandaloneDenialCompletedJournalSameSQLRestart(t *testing.T) {
	r, cfg, p, first := denialFixture(t)
	adapter, err := forgetops.NewCanonicalForgetAdapter(r.service.store)
	requireNoError(t, err, "adapter: %v")

	command := denialCommand(first.Scope)
	outcome, err := adapter.ForgetScope(t.Context(), command)
	requireNoError(t, err, "canonical complete before crash: %v")
	db := openRuntimeSQL(t, r)
	denialAssertCounts(t, db, first.Scope, "", true)
	requireNoError(t, db.Close(), "inspection close: %v")
	requireNoError(t, r.Stop(context.Background()), "stop before retirement: %v")

	calls := p.passages.Load()
	cfg.Embeddings = EmbeddingsConfig{}
	reopened, err := New(cfg)
	requireNoError(t, err, "same-SQL reopen: %v")
	t.Cleanup(func() { _ = reopened.Stop(context.Background()) })
	startSQLComponents(t, reopened)

	db = openRuntimeSQL(t, reopened)
	defer func() { _ = db.Close() }()

	denialAssertCounts(t, db, first.Scope, "", false)

	if p.passages.Load() != calls || reopened.service.semantic != nil {
		t.Fatal("disabled startup used provider")
	}

	replay, err := reopened.ForgetScope(t.Context(), command)
	requireNoError(t, err, "restart replay: %v")

	if !reflect.DeepEqual(outcome, replay) {
		t.Fatal("startup changed receipt")
	}
}

//nolint:cyclop,funlen,gocognit // Partial retirement and eligible later turn share one persisted database.
func TestStandaloneDenialInterruptedJoinSameSQLRestart(t *testing.T) {
	for _, boundary := range []string{"lexical-page", "before-sync", "after-activation"} {
		t.Run(boundary, func(t *testing.T) {
			r, cfg, _, first := denialFixture(t)

			db := openRuntimeSQL(t, r)
			if boundary == "lexical-page" {
				_, err := db.ExecContext(t.Context(), `CREATE TRIGGER denial_restart_failure BEFORE DELETE ON lexical_record
 BEGIN SELECT RAISE(ABORT,'private restart fault'); END`)
				requireNoError(t, err, "interrupt after semantic retirement: %v")
			}

			if boundary == "before-sync" {
				adapter, err := forgetops.NewCanonicalForgetAdapter(r.service.store)
				requireNoError(t, err, "adapter: %v")
				r.service.forget, err = memoryops.NewForgetServiceWithProjection(adapter,
					&forgetProjectionSyncer{retire: r.service.index, next: denialSyncFunc(func(context.Context, memory.Scope) error {
						return memory.RetryableError(memory.CodeConflict, "interrupted lexical sync", nil)
					})})
				requireNoError(t, err, "before-sync fault: %v")
			}

			command := denialCommand(first.Scope)

			outcome, err := r.ForgetScope(t.Context(), command)
			if (err == nil) != (boundary == "after-activation") || outcome.OperationID == "" {
				t.Fatalf("interrupted outcome/error=%+v/%v", outcome, err)
			}

			counts := denialCounts(t, db, first.Scope, "")
			if counts["semantic_chunk"] != 0 || counts["semantic_coverage"] != 0 {
				t.Fatal("fixture did not cross semantic retirement before interruption")
			}

			if boundary == "lexical-page" {
				if counts["lexical_record"] == 0 {
					t.Fatal("fixture lost unfinished lexical retirement")
				}

				_, err = db.ExecContext(t.Context(), "DROP TRIGGER denial_restart_failure")
				requireNoError(t, err, "remove fault before restart: %v")
			}

			_, later := denialSeedPopulation(t, r, first.Scope, "restart-later", 1, false)

			control, controlErr := r.service.reader.LoadRecallRecords(t.Context(), first.Scope, []string{later[0].RevisionID})
			if controlErr != nil || len(control) != 1 {
				t.Fatalf("eligible canonical control before restart: %+v %v", control, controlErr)
			}

			requireNoError(t, db.Close(), "close inspection: %v")
			requireNoError(t, r.Stop(context.Background()), "interrupt stop: %v")

			cfg.Embeddings = EmbeddingsConfig{}
			reopened, err := New(cfg)
			requireNoError(t, err, "same SQL: %v")
			t.Cleanup(func() { _ = reopened.Stop(context.Background()) })
			startSQLComponents(t, reopened)

			db = openRuntimeSQL(t, reopened)
			defer func() { _ = db.Close() }()

			var count, eligible int
			requireNoError(t, db.QueryRowContext(t.Context(),
				"SELECT count(*) FROM lexical_record WHERE revision_id<>?", later[0].RevisionID).Scan(&count),
				"old target population: %v")
			requireNoError(t, db.QueryRowContext(t.Context(),
				"SELECT count(*) FROM lexical_record WHERE revision_id=?", later[0].RevisionID).Scan(&eligible),
				"later population: %v")

			if count != 0 || eligible == 0 {
				t.Fatalf("restart old/eligible=%d/%d", count, eligible)
			}

			replay, err := reopened.ForgetScope(t.Context(), command)
			requireNoError(t, err, "restart original replay: %v")

			if !reflect.DeepEqual(replay, outcome) {
				t.Fatal("interruption/restart changed original outcome")
			}

			response, searchErr := reopened.Search(t.Context(),
				memory.RecallRequest{Scope: first.Scope, Query: "Bishkek", Limit: 10})
			requireNoError(t, searchErr, "later recall after old-request replay: %v")

			if len(response.Results) != 1 || response.Results[0].Text != "Bishkek memory restart-later-00000" {
				t.Fatalf("later turn lost across interrupted join: %+v", response)
			}
		})
	}
}

//nolint:cyclop // Both durable pending stages must finish canonically before SQL readiness.
func TestStandaloneDenialPendingJournalRecovery(t *testing.T) {
	for _, stage := range []memory.ScopeEraseStage{memory.ScopeErasePrepared, memory.ScopeEraseScrubMessages} {
		t.Run(string(stage), func(t *testing.T) {
			r, cfg, _, first := denialFixture(t)
			command := denialCommand(first.Scope)
			request := denialEraseRequest(t, r, command)
			journal, err := runtimeStore(t, r).BeginScopeErase(t.Context(), request)
			requireNoError(t, err, "pending begin: %v")

			for attempts := 0; journal.Stage != stage && attempts < 100; attempts++ {
				journal, err = r.service.store.AdvanceScopeErase(t.Context(), request.Scope, request.OperationID)
				requireNoError(t, err, "pending advance: %v")
			}

			if journal.Stage != stage {
				t.Fatalf("missing crash seam %s: %+v", stage, journal)
			}

			if stage == memory.ScopeEraseScrubMessages && journal.Outcome == nil {
				t.Fatal("missing persisted outcome before Complete")
			}

			db := openRuntimeSQL(t, r)
			denialAssertCounts(t, db, first.Scope, "", true)
			requireNoError(t, db.Close(), "close inspection: %v")
			requireNoError(t, r.Stop(context.Background()), "pending crash stop: %v")

			cfg.Embeddings = EmbeddingsConfig{}
			reopened, err := New(cfg)
			requireNoError(t, err, "pending reopen: %v")
			t.Cleanup(func() { _ = reopened.Stop(context.Background()) })
			startSQLComponents(t, reopened)
			complete, found, err := reopened.service.store.LoadScopeErase(t.Context(), request.Scope, request.OperationID)
			requireNoError(t, err, "complete journal: %v")

			if !found || complete.Stage != memory.ScopeEraseComplete || complete.Outcome == nil {
				t.Fatalf("ready with pending canonical work: %+v", complete)
			}

			if journal.Outcome != nil && !reflect.DeepEqual(*journal.Outcome, *complete.Outcome) {
				t.Fatal("pending recovery changed persisted receipt")
			}

			db = openRuntimeSQL(t, reopened)
			defer func() { _ = db.Close() }()

			denialAssertCounts(t, db, first.Scope, "", false)
			replay, err := reopened.ForgetScope(t.Context(), command)
			requireNoError(t, err, "recovered public replay: %v")

			if !reflect.DeepEqual(replay, *complete.Outcome) {
				t.Fatal("recovered receipt changed")
			}
		})
	}
}

func denialEraseRequest(t *testing.T, r *Runtime, command memory.ForgetScopeCommand) memory.ScopeEraseRequest {
	t.Helper()

	id, err := memory.CanonicalForgetOperationID(
		memory.ForgetKindScope, command.Scope, memory.SourceRef{}, command.RequestID)
	requireNoError(t, err, "operation: %v")
	state, err := r.service.store.LoadScopeState(t.Context(), command.Scope)
	requireNoError(t, err, "erase boundary: %v")

	return memory.ScopeEraseRequest{Scope: command.Scope, OperationID: id,
		Fingerprint: memory.CanonicalForgetFingerprint(
			id, command.Scope, memory.ForgetKindScope, memory.SourceRef{}, command.RequestID),
		ExpectedScopeVersion: state.Version, ForgottenAt: command.ForgottenAt}
}

func TestStandaloneDenialInvalidOwnerStopsStartup(t *testing.T) {
	p := &lifecycleEmbeddingProvider{}
	r, err := New(enabledLifecycleConfig(t, p))
	requireNoError(t, err, "new: %v")
	t.Cleanup(func() { _ = r.Stop(context.Background()) })
	turn := seedSemanticTurn(t, r, "invalid-owner")

	db := openRuntimeSQL(t, r)
	defer func() { _ = db.Close() }()

	_, err = db.ExecContext(t.Context(), `INSERT INTO semantic_build
 (build_id,scope_kind,scope_key,generation_id,physical_build_id,watermark,model_space,dimensions,epoch,status)
 VALUES('invalid-owner','personal','orphan','generation','physical',zeroblob(8),'model',2,zeroblob(8),'building')`)
	requireNoError(t, err, "invalid persisted scope control: %v")

	err = r.service.startComponentsUntilShutdown(t.Context())
	if err == nil || r.Ready() || !strings.Contains(err.Error(), "back up") ||
		!strings.Contains(err.Error(), "rebuild") || p.calls.Load() != 0 {
		t.Fatalf("invalid ownership became optional fallback: %v ready=%t calls=%d", err, r.Ready(), p.calls.Load())
	}

	_, found, err := r.service.store.LoadActiveProjectionManifest(t.Context(), turn.Scope, "sqlite")
	requireNoError(t, err, "pre-lexical ordering: %v")

	if found {
		t.Fatal("invalid ownership reached lexical certification")
	}
}

//nolint:cyclop // Required failure, preserved certificate and operator retry are one startup sequence.
func TestStandaloneDenialStartupOrder(t *testing.T) {
	r, cfg, p, first := denialFixture(t)
	adapter, err := forgetops.NewCanonicalForgetAdapter(r.service.store)
	requireNoError(t, err, "adapter: %v")

	command := denialCommand(first.Scope)
	outcome, err := adapter.ForgetScope(t.Context(), command)
	requireNoError(t, err, "canonical complete: %v")
	before, found, err := r.service.store.LoadActiveProjectionManifest(t.Context(), first.Scope, "sqlite")
	requireNoError(t, err, "manifest before privacy pass: %v")

	if !found {
		t.Fatal("missing certified pre-crash source")
	}

	db := openRuntimeSQL(t, r)
	_, err = db.ExecContext(t.Context(), `UPDATE semantic_chunk SET embedding=zeroblob(8);
CREATE TRIGGER denial_startup_failure BEFORE DELETE ON semantic_chunk
BEGIN SELECT RAISE(ABORT,'private denial fault'); END`)
	requireNoError(t, err, "metadata-only startup fault: %v")
	requireNoError(t, db.Close(), "inspection close: %v")
	requireNoError(t, r.Stop(context.Background()), "stop: %v")

	calls := p.passages.Load()
	cfg.Embeddings = EmbeddingsConfig{}
	reopened, err := New(cfg)
	requireNoError(t, err, "disabled invalid-vector reopen: %v")
	t.Cleanup(func() { _ = reopened.Stop(context.Background()) })

	err = reopened.service.startComponentsUntilShutdown(t.Context())
	if err == nil || reopened.Ready() || !strings.Contains(err.Error(), "back up") ||
		!strings.Contains(err.Error(), "rebuild") || strings.Contains(err.Error(), "private denial fault") {
		t.Fatalf("required privacy startup failure/guidance: %v", err)
	}

	after, found, err := reopened.service.store.LoadActiveProjectionManifest(t.Context(), first.Scope, "sqlite")
	requireNoError(t, err, "unchanged pre-lexical certificate: %v")

	if !found || before != after || p.passages.Load() != calls {
		t.Fatal("lexical/provider work preceded required cleanup")
	}

	db = openRuntimeSQL(t, reopened)
	defer func() { _ = db.Close() }()

	_, err = db.ExecContext(t.Context(), "DROP TRIGGER denial_startup_failure")
	requireNoError(t, err, "remove controlled fault: %v")
	startSQLComponents(t, reopened)
	denialAssertCounts(t, db, first.Scope, "", false)
	replay, err := reopened.ForgetScope(t.Context(), command)
	requireNoError(t, err, "startup retry: %v")

	if !reflect.DeepEqual(outcome, replay) {
		t.Fatal("failed startup rewrote canonical receipt")
	}
}
