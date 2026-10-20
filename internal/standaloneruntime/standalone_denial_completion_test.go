//go:build integration

package standaloneruntime //nolint:testpackage // Tests the real public completion boundary.

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/forgetops"
	"github.com/baldaworks/boyan/internal/memoryops"
	"github.com/baldaworks/boyan/pkg/memory"
)

// Removing the forget-only join leaves retained lexical history and the old
// positive semantic publication behind despite a successful public outcome.
func TestStandaloneDenialPublicScopeAndReplay(t *testing.T) {
	r, _, p, first := denialFixture(t)

	db := openRuntimeSQL(t, r)
	defer func() { _ = db.Close() }()

	p.blocked.Store(true)
	seedSemanticTurn(t, r, "denial-provider-paused")
	syncer := semanticSyncer{lexical: r.service.projection, manager: r.service.semantic.manager}
	requireNoError(t, syncer.Sync(t.Context(), first.Scope), "schedule blocked passage: %v")

	select {
	case <-p.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("passage provider not paused")
	}

	command := denialCommand(first.Scope)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	outcome, err := r.ForgetScope(ctx, command)
	requireNoError(t, err, "public scope erase while provider blocked: %v")
	denialAssertCounts(t, db, first.Scope, "", false)

	command.ForgottenAt = command.ForgottenAt.Add(time.Hour)
	replay, err := r.ForgetScope(ctx, command)
	requireNoError(t, err, "public replay: %v")

	if !reflect.DeepEqual(outcome, replay) {
		t.Fatal("replay changed canonical outcome")
	}

	denialAssertCounts(t, db, first.Scope, "", false)
	response, err := r.Search(ctx, memory.RecallRequest{Scope: first.Scope, Query: "Bishkek", Limit: 10})
	requireNoError(t, err, "hydration after denial: %v")

	if len(response.Results) != 0 {
		t.Fatal("erased canonical text released")
	}

	select {
	case <-p.release:
		t.Fatal("cleanup required provider release")
	default:
	}
}

//nolint:funlen // The two public calls bracket one canonical-fence interleaving and its restart.
func TestStandaloneDenialSourceAndLaterTurn(t *testing.T) {
	for _, migrated := range []bool{false, true} {
		t.Run(fmt.Sprint("migrated=", migrated), func(t *testing.T) {
			p := &denialProvider{}
			cfg := enabledLifecycleConfig(t, p)
			r, err := New(cfg)
			requireNoError(t, err, "new: %v")
			t.Cleanup(func() { _ = r.Stop(context.Background()) })

			scope := memory.Scope{Kind: memory.ScopeKindPersonal, Key: "denial:source"}
			command, targets := denialSeedPopulation(t, r, scope, "target", 1, migrated)
			_, peers := denialSeedPopulation(t, r, scope, "peer", 1, false)
			startSQLComponents(t, r)
			waitSemanticState(t, r, scope, "hybrid-ready")
			r.service.semantic.startup.Wait()
			denialHealthy(t, r, scope, 2)

			db := openRuntimeSQL(t, r)
			defer func() { _ = db.Close() }()

			denialAssertCounts(t, db, scope, targets[0].RevisionID, true)
			denialAssertCounts(t, db, scope, peers[0].RevisionID, true)

			adapter, err := forgetops.NewCanonicalForgetAdapter(r.service.store)
			requireNoError(t, err, "adapter: %v")

			var later []memory.MemoryRevision

			called := false
			next := semanticSyncer{lexical: r.service.projection, manager: r.service.semantic.manager}
			joined := &forgetProjectionSyncer{retire: denialRetireFunc(func(ctx context.Context, got memory.Scope) error {
				if !called {
					called = true
					// Adapter has completed and released its canonical fence, but
					// SQL retirement has not begun. This must remain a valid turn.
					_, later = denialSeedPopulation(t, r, scope, "later", 1, false)
					requireNoError(t, r.service.projection.Sync(ctx, scope), "later projection: %v")
					requireNoError(t, r.service.semantic.manager.Repair(ctx, scope), "later semantic publication: %v")
					denialAssertCounts(t, db, scope, later[0].RevisionID, true)
				}

				return r.service.index.ReconcileCanonicalDenials(ctx, got)
			}), next: next}
			r.service.forget, err = memoryops.NewForgetServiceWithProjection(adapter, joined)
			requireNoError(t, err, "joined seam: %v")
			outcome, err := r.ForgetSource(t.Context(), command)
			requireNoError(t, err, "public source erase: %v")

			if !called {
				t.Fatal("source bypassed joined retirement")
			}

			denialAssertCounts(t, db, scope, targets[0].RevisionID, false)
			denialAssertCounts(t, db, scope, peers[0].RevisionID, true)
			denialAssertCounts(t, db, scope, later[0].RevisionID, true)
			denialHealthy(t, r, scope, 2)

			command.ForgottenAt = command.ForgottenAt.Add(time.Hour)
			replay, err := r.ForgetSource(t.Context(), command)
			requireNoError(t, err, "source original journal replay: %v")

			if !reflect.DeepEqual(replay, outcome) {
				t.Fatal("later turn changed source outcome")
			}

			denialAssertCounts(t, db, scope, targets[0].RevisionID, false)
			denialAssertCounts(t, db, scope, later[0].RevisionID, true)
			denialHealthy(t, r, scope, 2)

			if migrated {
				if _, err := r.service.store.CanonicalSourceID(t.Context(), scope, command.Source); err == nil {
					t.Fatal("migration resolver unexpectedly has erased payload")
				}

				requireNoError(t, db.Close(), "source inspection close: %v")
				requireNoError(t, r.Stop(context.Background()), "source stop: %v")

				reopened, openErr := New(cfg)
				requireNoError(t, openErr, "source same-SQL reopen: %v")
				t.Cleanup(func() { _ = reopened.Stop(context.Background()) })
				startSQLComponents(t, reopened)
				waitSemanticState(t, reopened, scope, "hybrid-ready")
				replay, err = reopened.ForgetSource(t.Context(), command)
				requireNoError(t, err, "migrated restart replay: %v")

				if !reflect.DeepEqual(replay, outcome) {
					t.Fatal("migrated restart changed original receipt")
				}

				denialHealthy(t, reopened, scope, 2)
			}
		})
	}
}

func TestStandaloneDenialTruncatedLargeScope(t *testing.T) {
	r, err := New(testStandaloneConfig(t))
	requireNoError(t, err, "new: %v")
	t.Cleanup(func() { _ = r.Stop(context.Background()) })

	scope := memory.Scope{Kind: memory.ScopeKindPersonal, Key: "denial:large"}
	_, revisions := denialSeedPopulation(t, r, scope, "large", 8200, false)
	startSQLComponents(t, r)

	db := openRuntimeSQL(t, r)
	defer func() { _ = db.Close() }()

	var before int
	requireNoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM lexical_record").Scan(&before),
		"large population: %v")

	if before != len(revisions) || before <= 8192 {
		t.Fatalf("large lexical population=%d", before)
	}

	command := denialCommand(scope)
	outcome, err := r.ForgetScope(t.Context(), command)
	requireNoError(t, err, "large public erase: %v")

	if !outcome.ReferencesTruncated || len(outcome.Sources) != 0 || len(outcome.Revisions) != 0 {
		t.Fatalf("not truncated: %+v", outcome)
	}

	for _, table := range []string{"lexical_record", "lexical_fts", "lexical_source", "lexical_conversation"} {
		var count int
		requireNoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&count),
			"large final family: %v")

		if count != 0 {
			t.Fatalf("large %s=%d", table, count)
		}
	}

	replay, err := r.ForgetScope(t.Context(), command)
	requireNoError(t, err, "large replay: %v")

	if !reflect.DeepEqual(replay, outcome) {
		t.Fatal("large original outcome changed")
	}

	assertSQLSemanticTables(t, r.service.state.Root, 0)
}
