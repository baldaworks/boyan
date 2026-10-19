package standaloneruntime //nolint:testpackage // Real SQL recovery faults exercise runtime cleanup and durable fences.

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestSQLRuntimePendingEraseFailureRetainsAuthority(t *testing.T) {
	forEachSQLRuntimeConfig(t, func(t *testing.T, cfg Config) {
		runtime, err := New(cfg)
		requireNoError(t, err, "new: %v")
		turn := seedSemanticTurn(t, runtime, "pending-native")
		state, err := runtime.service.store.LoadScopeState(t.Context(), turn.Scope)
		requireNoError(t, err, "state: %v")
		original, err := runtimeStore(t, runtime).BeginScopeErase(t.Context(), m.ScopeEraseRequest{
			Scope: turn.Scope, OperationID: "pending-native", Fingerprint: "pending-native",
			ExpectedScopeVersion: state.Version, ForgottenAt: time.Now().UTC()})
		requireNoError(t, err, "begin: %v")
		requireNoError(t, runtime.Stop(t.Context()), "stop pending: %v")
		remove := failRuntimeEraseJournal(t, cfg)
		runtime, err = New(cfg)
		requireNoError(t, err, "reopen pending: %v")

		if err := runtime.Start(t.Context()); err == nil || runtime.Ready() {
			t.Fatal("failed erase recovery became ready")
		}

		requireNoError(t, runtime.Stop(t.Context()), "failed startup cleanup: %v")
		store := openFixtureSQLStore(t, cfg)
		journal, found, err := store.LoadScopeErase(t.Context(), original.Scope, original.OperationID)
		requireNoError(t, err, "failed journal: %v")

		if !found || !reflect.DeepEqual(journal, original) {
			t.Fatal("failed page advanced durable journal")
		}

		if err := store.PrepareSourcePrivacy(t.Context()); err == nil {
			// A preexisting ready marker is allowed, but it must never waive the
			// actual erase fence on an ordinary mutation.
			_, writeErr := store.CommitCanonicalOperation(t.Context(), m.CanonicalOperationCommitRequest{
				Scope: original.Scope, OperationID: "forbidden", Fingerprint: "forbidden", ExpectedScopeVersion: state.Version,
				CommittedAt: time.Now().UTC()})
			if writeErr == nil {
				t.Fatal("pending erase lost writer fence")
			}
		}

		requireNoError(t, store.Close(), "close inspection owner: %v")
		remove()

		runtime, err = New(cfg)
		requireNoError(t, err, "reopen repaired: %v")
		t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
		requireNoError(t, runtime.Start(t.Context()), "recover real pending erase: %v")
		assertStandaloneRecoveredErase(t, runtime, original)
	})
}

func failRuntimeEraseJournal(t *testing.T, cfg Config) func() {
	t.Helper()

	p := nativeRuntimeProbe(t, cfg)
	if cfg.Canonical.Backend == "postgres" {
		requireNoError(t, p.exec(`CREATE FUNCTION boyan_canonical.task17_erase_fail() RETURNS trigger
 LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'task17 erase fault'; END $$`), "erase fault function: %v")
		requireNoError(t, p.exec(`CREATE TRIGGER task17_erase_fail BEFORE UPDATE ON boyan_canonical.erase_journal
 FOR EACH ROW EXECUTE FUNCTION boyan_canonical.task17_erase_fail()`), "erase trigger: %v")

		return func() {
			err := p.exec("DROP TRIGGER task17_erase_fail ON boyan_canonical.erase_journal")
			requireNoError(t, err, "remove erase trigger: %v")
			requireNoError(t, p.exec("DROP FUNCTION boyan_canonical.task17_erase_fail()"), "remove erase function: %v")
		}
	}

	requireNoError(t, p.exec(`CREATE TRIGGER task17_erase_fail BEFORE UPDATE ON erase_journal
 BEGIN SELECT RAISE(ABORT,'task17 erase fault'); END`), "erase trigger: %v")

	return func() { requireNoError(t, p.exec("DROP TRIGGER task17_erase_fail"), "remove erase trigger: %v") }
}

func TestSQLRuntimeStructuralCorruptionFailsStartup(t *testing.T) {
	for _, test := range []struct{ backend, mode string }{
		{"sqlite", "stamp"}, {"postgres", "stamp"}, {"postgres", "frame"}, {"postgres", "duplicate"},
	} {
		t.Run(test.backend+"/"+test.mode, func(t *testing.T) {
			cfg := sqlRuntimeConfig(t, test.backend)
			mode := test.mode
			runtime, err := New(cfg)
			requireNoError(t, err, "new: %v")
			turn := seedSemanticTurn(t, runtime, "corruption")
			p := nativeRuntimeProbe(t, cfg)

			switch mode {
			case "stamp":
				err = p.exec("UPDATE "+p.table("row_stamp")+" SET deleted=true WHERE family=$1", []byte("scope"))
				requireNoError(t, err, "corrupt stamp: %v")
			case "frame":
				err = p.exec("UPDATE boyan_canonical.scope_state SET logical_key=$1", []byte("broken-frame"))
				requireNoError(t, err, "corrupt frame: %v")
			case "duplicate":
				requireNoError(t, p.exec(`INSERT INTO boyan_canonical.scope_state
 (logical_key,scope_partition,scope_key,scope_kind,envelope,version_u64,change_seq_u64)
 SELECT logical_key,scope_partition,scope_key,scope_kind,envelope,version_u64,change_seq_u64
 FROM boyan_canonical.scope_state WHERE scope_key=$1`, []byte(turn.Scope.Key)), "duplicate identity: %v")
			}

			if err := runtime.Start(t.Context()); err == nil || runtime.Ready() {
				t.Fatal("corrupt canonical state reached readiness")
			}

			requireNoError(t, runtime.Stop(t.Context()), "failed startup released resources: %v")

			if reopened, err := New(cfg); err == nil {
				_ = reopened.Stop(t.Context())
				t.Fatal("reopen silently repaired corrupt canonical state")
			}
		})
	}
}

func TestSQLRuntimeSerializesDuplicateAdmissions(t *testing.T) {
	forEachSQLRuntimeConfig(t, func(t *testing.T, cfg Config) {
		runtime, err := New(cfg)
		requireNoError(t, err, "new: %v")
		t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
		requireNoError(t, runtime.Start(t.Context()), "start: %v")

		request := m.CanonicalOperationCommitRequest{Scope: m.Scope{Key: "runtime:duplicate", Kind: m.ScopeKindPersonal},
			OperationID: "same", Fingerprint: "same", CommittedAt: time.Now().UTC()}

		var workers sync.WaitGroup

		outcomes := make([]m.CanonicalMutationOutcome, 2)
		errors := make([]error, 2)

		for i := range outcomes {
			workers.Go(func() { outcomes[i], errors[i] = runtime.service.store.CommitCanonicalOperation(t.Context(), request) })
		}

		workers.Wait()

		for _, err := range errors {
			requireNoError(t, err, "serialized replay: %v")
		}

		if !reflect.DeepEqual(outcomes[0], outcomes[1]) || outcomes[0].ScopeVersion != 1 {
			t.Fatal("duplicate admissions changed canonical identity")
		}

		requireNoError(t, runtime.Stop(t.Context()), "stop: %v")
		runtime, err = New(cfg)
		requireNoError(t, err, "reopen: %v")
		requireNoError(t, runtime.Start(t.Context()), "restart: %v")
		changes, err := runtime.service.store.ScanScopeChanges(t.Context(), request.Scope, 0, 10)
		requireNoError(t, err, "durable changes: %v")

		if len(changes) != 1 {
			t.Fatalf("duplicate admission committed %d changes", len(changes))
		}
	})
}
