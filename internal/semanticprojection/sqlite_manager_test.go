//nolint:cyclop,funlen,gocognit,lll,testpackage // Real SQL fault matrices retain explicit boundary and row-family assertions.
package semanticprojection

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sqlitestore "github.com/baldaworks/boyan/index/sqlite"
	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
	sqlitedriver "modernc.org/sqlite"
)

// Removing exact owner cleanup leaves durable rows after each rejected build.
func TestSQLiteManagerReleasesEveryStageFailure(t *testing.T) {
	for _, boundary := range []string{
		"BEFORE INSERT ON semantic_build", "BEFORE INSERT ON semantic_coverage",
		"BEFORE INSERT ON semantic_chunk", "BEFORE UPDATE OF complete ON semantic_build",
	} {
		t.Run(boundary, func(t *testing.T) {
			f := newSQLiteManagerFixture(t)

			condition := ""
			if strings.Contains(boundary, "UPDATE") {
				condition = " WHEN NEW.complete=1"
			}

			f.exec(t, "CREATE TRIGGER fail_stage "+boundary+condition+" BEGIN SELECT RAISE(ABORT,'private provider prose'); END")

			for attempt := range 3 {
				if err := f.manager.Repair(t.Context(), f.scope); err == nil || strings.Contains(err.Error(), "private") {
					t.Fatalf("unsafe Stage failure: %v", err)
				}

				if f.store.generation.BuildID == "" || f.store.releases.Load() != int32(attempt+1) {
					t.Fatal("Stage attempt did not finish exactly one terminal guard")
				}

				f.assertFamilies(t, f.store.generation.BuildID, 0, 0, 0)

				if !strings.Contains(boundary, "semantic_build") || strings.Contains(boundary, "UPDATE") {
					state, found, err := f.store.ActiveState(t.Context(), f.scope)
					if err != nil || !found || state.Status != semanticcore.StatusDegraded || state.BuildID == f.store.generation.BuildID {
						t.Fatalf("fresh diagnostic marker=%#v %v %v", state, found, err)
					}

					f.assertFamilies(t, "", 1, 0, 0)
				}
			}
		})
	}
}

// A fixed bleve identity would reject this real sqlite physical certificate.
func TestSQLiteManagerBindsProjectionIdentityAndRequiresLifecycle(t *testing.T) {
	f := newSQLiteManagerFixture(t)
	if err := f.manager.Repair(t.Context(), f.scope); err != nil {
		t.Fatal(err)
	}

	status, err := f.manager.Status(t.Context(), f.scope)
	if err != nil || status.State != StateReady {
		t.Fatalf("status=%#v err=%v", status, err)
	}

	// The default and explicitly composed SQLite identity both require the
	// exact owner capability; the temporary legacy default no longer exists.
	for _, projectionID := range []string{"", "sqlite"} {
		for _, lifecycle := range []CandidateLifecycle{nil, (*sqlitestore.Store)(nil)} {
			deps := f.dependencies()
			deps.Lifecycle = lifecycle
			deps.LexicalProjectionID = projectionID

			manager, err := New(deps)
			if !errors.Is(err, semanticcore.ErrInvalid) {
				if manager != nil {
					_ = manager.Close(t.Context())
				}

				t.Fatalf("missing sqlite capability accepted: %v", err)
			}
		}
	}
}

// Canonical and physical changes must each abandon only this owned candidate.
func TestSQLiteManagerReleasesStaleAndCanceledCandidates(t *testing.T) {
	for _, change := range []string{"canonical", "physical", "model", "canceled", "deadline"} {
		t.Run(change, func(t *testing.T) {
			f := newSQLiteManagerFixture(t)

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			want := ErrStaleView
			f.manager.beforeFinalCheck = func() {
				switch change {
				case "canonical":
					f.canonical.setSequence(8)
				case "physical":
					f.exec(t, "UPDATE lexical_build SET revoked=1")
				case "model":
					f.manager.spaceID = "changed-model"
				case "canceled":
					cancel()
				case "deadline":
					<-ctx.Done()
				}
			}

			if change == "canceled" {
				want = context.Canceled
			}

			if change == "deadline" {
				var deadlineCancel context.CancelFunc

				ctx, deadlineCancel = context.WithTimeout(t.Context(), 100*time.Millisecond)
				defer deadlineCancel()

				want = context.DeadlineExceeded
			}

			if err := f.manager.Repair(ctx, f.scope); !errors.Is(err, want) {
				t.Fatalf("Repair=%v want=%v", err, want)
			}

			f.assertFamilies(t, "", 0, 0, 0)
		})
	}
}

// The owner must not confuse a failed return with proof that commit rolled back.
func TestSQLiteManagerResolvesAmbiguousPublication(t *testing.T) {
	for _, outcome := range []string{"rollback", "committed-error", "committed-cancel", "committed-deadline"} {
		t.Run(outcome, func(t *testing.T) {
			f := newSQLiteManagerFixture(t)

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			primary := errors.New("publication transport failure")
			if outcome == "committed-cancel" {
				primary = context.Canceled
			}

			if outcome == "committed-deadline" {
				primary = context.DeadlineExceeded
			}

			f.store.publish = func(ctx context.Context, publication semanticcore.Publication) error {
				if outcome != "rollback" {
					if err := f.store.Store.Publish(ctx, publication); err != nil {
						t.Fatal(err)
					}
				}

				if outcome == "committed-cancel" {
					cancel()
				}

				return primary
			}
			if err := f.manager.Repair(ctx, f.scope); !errors.Is(err, primary) {
				t.Fatalf("primary identity lost: %v", err)
			}

			controls := 1
			if outcome == "rollback" {
				controls = 0
			}

			f.assertFamilies(t, "", controls, 0, 0)

			if outcome != "rollback" {
				state, found, err := f.store.ActiveState(t.Context(), f.scope)
				if err != nil || !found || state.Status != semanticcore.StatusDegraded || state.PublicationEpoch != 1 {
					t.Fatalf("ambiguous active state=%#v found=%v err=%v", state, found, err)
				}
			}
		})
	}
}

// Both the CAS loser and a retired owner must leave all peer data intact.
func TestSQLiteManagerPreservesCompetingPublicationAndPendingPeer(t *testing.T) {
	for _, order := range []string{"peer-first", "owner-first", "after-classification"} {
		t.Run(order, func(t *testing.T) {
			f := newSQLiteManagerFixture(t)

			var winner semanticcore.Generation

			f.manager.beforePublish = func() {
				f.stageCopy(t, "pending-peer", f.manager.spaceID)

				winner = f.stageCopy(t, "winning-peer", hashText("different-model"))
				if order == "peer-first" {
					f.publishPeer(t, winner, "", 0)
				}
			}

			f.store.publish = func(ctx context.Context, publication semanticcore.Publication) error {
				if err := f.store.Store.Publish(ctx, publication); err != nil {
					return err
				}

				if order == "owner-first" {
					f.publishPeer(t, winner, publication.BuildID, 1)
				}

				return semanticcore.ErrConflict
			}
			if order == "after-classification" {
				f.store.release = func(ctx context.Context, scope memory.Scope, build string) (semanticcore.CandidateRelease, error) {
					result, err := f.store.Store.ReleaseUnpublished(ctx, scope, build)
					if err != nil || result.Disposition != semanticcore.CandidatePublished {
						t.Fatalf("classification=%#v err=%v", result, err)
					}

					f.publishPeer(t, winner, build, result.Epoch)

					return result, nil
				}
			}

			if err := f.manager.Repair(t.Context(), f.scope); !errors.Is(err, semanticcore.ErrConflict) {
				t.Fatalf("CAS result=%v", err)
			}

			state, found, err := f.store.ActiveState(t.Context(), f.scope)
			if err != nil || !found || state.BuildID != "winning-peer" || state.Status != semanticcore.StatusReady {
				t.Fatalf("winner changed=%#v %v %v", state, found, err)
			}

			f.assertFamilies(t, f.store.generation.BuildID, 0, 0, 0)
			f.assertFamilies(t, "pending-peer", 1, 1, 1)
			f.assertFamilies(t, "winning-peer", 1, 1, 1)
		})
	}
}

// Exact invalidation must work after lexical authority has moved ahead.
func TestSQLiteManagerPostcheckInvalidatesObsoleteSourcePrecisely(t *testing.T) {
	for _, newerWinner := range []bool{false, true} {
		t.Run(map[bool]string{false: "owned", true: "newer-winner"}[newerWinner], func(t *testing.T) {
			f := newSQLiteManagerFixture(t)

			f.manager.afterPublish = func() {
				if newerWinner {
					winner := f.stageCopy(t, "newer-winner", f.manager.spaceID)
					f.publishPeer(t, winner, f.store.generation.BuildID, 1)
				}

				f.canonical.setSequence(8)
				manifest := activeManifest(f.scope, "new-source", 8)
				manifest.ProjectionID = "sqlite"
				f.checkpoints.set(manifest)
			}
			if err := f.manager.Repair(t.Context(), f.scope); !errors.Is(err, ErrStaleView) {
				t.Fatalf("postcheck=%v", err)
			}

			var status string

			if newerWinner {
				f.assertFamilies(t, "newer-winner", 1, 1, 1)

				if err := f.db.QueryRowContext(t.Context(), "SELECT status FROM semantic_build WHERE build_id='newer-winner'").Scan(&status); err != nil || status != "ready" {
					t.Fatalf("newer winner invalidated: %s %v", status, err)
				}
			} else {
				f.assertFamilies(t, "", 1, 0, 0)

				if err := f.db.QueryRowContext(t.Context(), "SELECT status FROM semantic_build").Scan(&status); err != nil || status != "degraded" {
					t.Fatalf("obsolete own publication remains ready: %s %v", status, err)
				}
			}
		})
	}
}

// Persistent cleanup failure must not mask the primary error or disappear from status.
func TestSQLiteManagerDefersCleanupWithPrimaryErrorAndSafeStatus(t *testing.T) {
	for _, boundary := range []string{"before-revocation", "after-revocation", "timeout", "closed"} {
		t.Run(boundary, func(t *testing.T) {
			f := newSQLiteManagerFixture(t)
			f.manager.cleanupTimeout = 20 * time.Millisecond
			f.manager.beforeFinalCheck = func() { f.canonical.setSequence(8) }

			switch boundary {
			case "before-revocation":
				f.exec(t, "CREATE TRIGGER fail_release BEFORE UPDATE OF complete ON semantic_build WHEN NEW.complete=0 BEGIN SELECT RAISE(ABORT,'private text'); END")
			case "after-revocation":
				f.exec(t, "CREATE TRIGGER fail_release BEFORE DELETE ON semantic_chunk BEGIN SELECT RAISE(ABORT,'private text'); END")
			case "timeout":
				f.store.release = func(ctx context.Context, _ memory.Scope, _ string) (semanticcore.CandidateRelease, error) {
					<-ctx.Done()

					return semanticcore.CandidateRelease{}, ctx.Err()
				}
			case "closed":
				f.store.release = func(ctx context.Context, scope memory.Scope, build string) (semanticcore.CandidateRelease, error) {
					if err := f.store.Store.Close(); err != nil {
						t.Fatal(err)
					}

					return f.store.Store.ReleaseUnpublished(ctx, scope, build)
				}
			}

			if err := f.manager.Repair(t.Context(), f.scope); !errors.Is(err, ErrStaleView) || strings.Contains(err.Error(), "private") {
				t.Fatalf("primary error=%v", err)
			}

			status, err := f.manager.Status(t.Context(), f.scope)
			if err != nil || !status.CleanupDeferred || status.CleanupReason != semanticcore.ReasonUnavailable || status.State != StateDegraded {
				t.Fatalf("deferred status=%#v err=%v", status, err)
			}

			if err := f.manager.Close(t.Context()); err != nil {
				t.Fatal(err)
			}

			status, err = f.manager.Status(t.Context(), f.scope)
			if err != nil || !status.CleanupDeferred || status.State != StateStopped {
				t.Fatalf("stopped status=%#v %v", status, err)
			}

			if boundary == "before-revocation" || boundary == "after-revocation" {
				f.exec(t, "DROP TRIGGER fail_release")

				if err := f.store.CollectGarbage(t.Context()); err != nil {
					t.Fatal(err)
				}

				if boundary == "before-revocation" {
					f.assertFamilies(t, "", 1, 1, 1)
					// All producers are now stopped: only startup repair can reclaim
					// a still-complete abandoned candidate.
					if err := f.store.RepairSemanticOrphans(t.Context()); err != nil {
						t.Fatal(err)
					}
				}

				f.assertFamilies(t, "", 0, 0, 0)
			}
		})
	}
}

// Cleanup must keep context values, share one budget, and preserve cancellation.
func TestSQLiteManagerCleanupSharesDetachedBudget(t *testing.T) {
	f := newSQLiteManagerFixture(t)
	f.manager.cleanupTimeout = 80 * time.Millisecond

	type contextKey struct{}

	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), contextKey{}, "retained"))
	defer cancel()

	var cleanupContext context.Context

	f.store.publish = func(ctx context.Context, publication semanticcore.Publication) error {
		if err := f.store.Store.Publish(ctx, publication); err != nil {
			t.Fatal(err)
		}

		cancel()

		return context.Canceled
	}
	f.store.release = func(ctx context.Context, scope memory.Scope, build string) (semanticcore.CandidateRelease, error) {
		cleanupContext = ctx
		if ctx.Err() != nil || ctx.Value(contextKey{}) != "retained" {
			t.Fatalf("cleanup lost parent values or inherited cancellation: %v", ctx.Err())
		}

		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 80*time.Millisecond {
			t.Fatalf("unbounded cleanup deadline=%v", deadline)
		}

		return f.store.Store.ReleaseUnpublished(ctx, scope, build)
	}
	f.store.invalidate = func(ctx context.Context, scope memory.Scope, build string, epoch uint64) error {
		if ctx != cleanupContext || epoch != 1 || build != f.store.generation.BuildID {
			t.Fatal("invalidation lost shared cleanup budget or own publication")
		}

		return f.store.Store.InvalidatePublication(ctx, scope, build, epoch)
	}
	// Fail precise invalidation with actual SQL, ensuring the original
	// cancellation survives and the committed ready data is reported deferred.
	f.exec(t, "CREATE TRIGGER fail_invalidation BEFORE DELETE ON semantic_coverage BEGIN SELECT RAISE(ABORT,'secret'); END")

	if err := f.manager.Repair(ctx, f.scope); !errors.Is(err, context.Canceled) {
		t.Fatalf("Repair=%v", err)
	}

	status, err := f.manager.Status(t.Context(), f.scope)
	if err != nil || status.State != StateDegraded || !status.CleanupDeferred {
		t.Fatalf("false ready=%#v err=%v", status, err)
	}

	f.assertFamilies(t, "", 1, 1, 1)
	// Later verified reuse cannot erase an outstanding cleanup obligation.
	f.exec(t, "DROP TRIGGER fail_invalidation")

	if err := f.manager.Repair(t.Context(), f.scope); err != nil {
		t.Fatal(err)
	}

	status, err = f.manager.Status(t.Context(), f.scope)
	if err != nil || status.State != StateDegraded || !status.CleanupDeferred {
		t.Fatalf("deferred flag lost=%#v %v", status, err)
	}
}

// Stop must retain the flight/slot/drain reservation until exact release finishes.
func TestSQLiteManagerStopWaitsForTerminalGuard(t *testing.T) {
	for _, scheduled := range []bool{false, true} {
		t.Run(map[bool]string{false: "repair", true: "scheduled"}[scheduled], func(t *testing.T) {
			testSQLiteManagerStopWaitsForTerminalGuard(t, scheduled)
		})
	}
}

func testSQLiteManagerStopWaitsForTerminalGuard(t *testing.T, scheduled bool) {
	t.Helper()

	f := newSQLiteManagerFixture(t)
	staged, cleanupEntered, allowCleanup := make(chan struct{}), make(chan struct{}), make(chan struct{})

	var buildContext context.Context

	f.manager.beforeBuild = func(ctx context.Context, _ memory.Scope) { buildContext = ctx }
	f.manager.beforeFinalCheck = func() { close(staged); <-buildContext.Done() }
	f.store.release = func(ctx context.Context, scope memory.Scope, build string) (semanticcore.CandidateRelease, error) {
		close(cleanupEntered)

		select {
		case <-allowCleanup:
		case <-ctx.Done():
			return semanticcore.CandidateRelease{}, ctx.Err()
		}

		return f.store.Store.ReleaseUnpublished(ctx, scope, build)
	}

	defer close(allowCleanup)

	repaired, stopped := make(chan error, 1), make(chan error, 1)

	if scheduled {
		if err := f.manager.Start(t.Context()); err != nil {
			t.Fatal(err)
		}

		if err := f.manager.Schedule(f.scope); err != nil {
			t.Fatal(err)
		}
	} else {
		go func() { repaired <- f.manager.Repair(t.Context(), f.scope) }()
	}

	<-staged

	go func() { stopped <- f.manager.Close(t.Context()) }()

	<-cleanupEntered

	if len(f.manager.buildSlots) != 1 {
		t.Fatal("build slot released before terminal attempt")
	}

	f.manager.flightMu.Lock()
	flights := len(f.manager.flights)
	f.manager.flightMu.Unlock()

	if flights != 1 {
		t.Fatal("flight released before terminal attempt")
	}

	select {
	case err := <-stopped:
		t.Fatalf("Stop crossed guard: %v", err)
	default:
	}

	allowCleanup <- struct{}{}

	if !scheduled {
		if err := <-repaired; !errors.Is(err, context.Canceled) {
			t.Fatalf("Repair=%v", err)
		}
	}

	if err := <-stopped; err != nil {
		t.Fatal(err)
	}

	f.assertFamilies(t, "", 0, 0, 0)

	if f.store.releases.Load() != 1 {
		t.Fatal("guard did not execute once")
	}
}

// A failed rebuild must leave the exact previous active generation unchanged.
func TestSQLiteManagerPreservesPreviousActiveOnAbandonedRebuild(t *testing.T) {
	f := newSQLiteManagerFixture(t)
	if err := f.manager.Repair(t.Context(), f.scope); err != nil {
		t.Fatal(err)
	}

	previous, found, err := f.store.ActiveState(t.Context(), f.scope)
	if err != nil || !found {
		t.Fatal(err)
	}

	f.manager.space = testModelSpace("new-model")

	f.manager.spaceID, err = semanticcore.SpaceFingerprint(f.manager.space)
	if err != nil {
		t.Fatal(err)
	}

	f.manager.beforeFinalCheck = func() {
		f.stageCopy(t, "independent-pending", f.manager.spaceID)
		f.canonical.setSequence(8)
	}
	if err := f.manager.Repair(t.Context(), f.scope); !errors.Is(err, ErrStaleView) {
		t.Fatalf("Repair=%v", err)
	}

	active, found, err := f.store.ActiveState(t.Context(), f.scope)
	if err != nil || !found || active != previous {
		t.Fatalf("previous changed=%#v %v %v", active, found, err)
	}

	f.assertFamilies(t, previous.BuildID, 1, 1, 1)
	f.assertFamilies(t, "independent-pending", 1, 1, 1)
	f.assertFamilies(t, f.store.generation.BuildID, 0, 0, 0)
}

var managerSQLFunctionSequence atomic.Uint64

// Close cancels the storage lifetime of an already-admitted guard operation.
func TestSQLiteManagerStoreCloseCancelsInFlightGuard(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})

	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()

	name := fmt.Sprintf("manager_cleanup_%d", managerSQLFunctionSequence.Add(1))
	if err := sqlitedriver.RegisterScalarFunction(name, 0, func(*sqlitedriver.FunctionContext, []driver.Value) (driver.Value, error) {
		close(entered)
		<-release

		return int64(0), nil
	}); err != nil {
		t.Fatal(err)
	}

	f := newSQLiteManagerFixture(t)
	f.exec(t, "CREATE TRIGGER pause_release BEFORE UPDATE OF complete ON semantic_build WHEN NEW.complete=0 BEGIN SELECT "+name+"(); END")
	f.manager.beforeFinalCheck = func() { f.canonical.setSequence(8) }

	repaired := make(chan error, 1)
	go func() { repaired <- f.manager.Repair(t.Context(), f.scope) }()

	<-entered

	// CloseContext initiates lifetime cancellation before checking its waiting
	// context. A canceled wait makes closing deterministic while SQL is paused;
	// the callback must still be released before the final drain can complete.
	closeContext, cancelClose := context.WithCancel(t.Context())
	cancelClose()

	if err := f.store.Store.CloseContext(closeContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("close initiation=%v", err)
	}

	unblock()

	if err := <-repaired; !errors.Is(err, ErrStaleView) {
		t.Fatalf("primary error=%v", err)
	}

	if err := f.store.Store.Close(); err != nil {
		t.Fatal(err)
	}

	status, err := f.manager.Status(t.Context(), f.scope)
	if err != nil || !status.CleanupDeferred {
		t.Fatalf("closed guard not deferred=%#v %v", status, err)
	}

	f.assertFamilies(t, "", 1, 1, 1)
}

func (f *sqliteManagerFixture) stageCopy(t *testing.T, id, space string) semanticcore.Generation {
	t.Helper()

	generation := f.store.generation
	generation.BuildID, generation.ModelSpace = id, space

	chunks := append([]semanticcore.ProjectionChunk(nil), f.store.chunks...)
	for i := range chunks {
		chunks[i].BuildID, chunks[i].ModelSpace = id, space
	}

	if _, err := f.store.Store.Stage(t.Context(), generation, f.store.coverage, chunks); err != nil {
		t.Fatal(err)
	}

	return generation
}

func (f *sqliteManagerFixture) publishPeer(t *testing.T, generation semanticcore.Generation, previous string, epoch uint64) {
	t.Helper()

	if err := f.store.Store.Publish(t.Context(), semanticcore.Publication{View: generation.View(), BuildID: generation.BuildID,
		ExpectedActiveBuildID: previous, ExpectedActiveEpoch: epoch}); err != nil {
		t.Fatal(err)
	}
}

type sqliteManagerCheckpoints struct {
	memory.ProjectionCheckpointStore
	*fakeManifestSource
}

func (c *sqliteManagerCheckpoints) LoadActiveProjectionManifest(ctx context.Context, scope memory.Scope,
	projectionID string) (memory.ProjectionManifest, bool, error) {
	return c.fakeManifestSource.LoadActiveProjectionManifest(ctx, scope, projectionID)
}

func (c *sqliteManagerCheckpoints) LoadProjectionManifest(ctx context.Context, scope memory.Scope,
	projectionID, generation string) (memory.ProjectionManifest, bool, error) {
	manifest, found, err := c.LoadActiveProjectionManifest(ctx, scope, projectionID)

	return manifest, found && manifest.GenerationID == generation, err
}

// Store wrappers inject returned failures without replacing actual durable writes.
type sqliteManagerStore struct {
	*sqlitestore.Store

	publish func(context.Context, semanticcore.Publication) error
	stage   func(context.Context, semanticcore.Generation, []semanticcore.CoverageRecord,
		[]semanticcore.ProjectionChunk) (semanticcore.State, error)
	release    func(context.Context, memory.Scope, string) (semanticcore.CandidateRelease, error)
	invalidate func(context.Context, memory.Scope, string, uint64) error
	releases   atomic.Int32
	generation semanticcore.Generation
	coverage   []semanticcore.CoverageRecord
	chunks     []semanticcore.ProjectionChunk
}

func (s *sqliteManagerStore) InvalidatePublication(ctx context.Context, scope memory.Scope, build string, epoch uint64) error {
	if s.invalidate != nil {
		return s.invalidate(ctx, scope, build, epoch)
	}

	return s.Store.InvalidatePublication(ctx, scope, build, epoch)
}

func (s *sqliteManagerStore) Stage(ctx context.Context, generation semanticcore.Generation,
	coverage []semanticcore.CoverageRecord, chunks []semanticcore.ProjectionChunk) (semanticcore.State, error) {
	s.generation = generation
	s.coverage = coverage

	s.chunks = chunks
	if s.stage != nil {
		return s.stage(ctx, generation, coverage, chunks)
	}

	return s.Store.Stage(ctx, generation, coverage, chunks)
}

func (s *sqliteManagerStore) Publish(ctx context.Context, publication semanticcore.Publication) error {
	if s.publish != nil {
		return s.publish(ctx, publication)
	}

	return s.Store.Publish(ctx, publication)
}

func (s *sqliteManagerStore) ReleaseUnpublished(ctx context.Context, scope memory.Scope,
	buildID string) (semanticcore.CandidateRelease, error) {
	s.releases.Add(1)

	if s.release != nil {
		return s.release(ctx, scope, buildID)
	}

	return s.Store.ReleaseUnpublished(ctx, scope, buildID)
}

type sqliteManagerFixture struct {
	scope       memory.Scope
	canonical   *fakeCanonical
	privacy     *sqliteFixturePrivacy
	checkpoints *sqliteManagerCheckpoints
	store       *sqliteManagerStore
	db          *sql.DB
	manager     *Manager
}

func newSQLiteManagerFixture(t *testing.T, changes ...func(*memory.RecallRecord)) *sqliteManagerFixture {
	t.Helper()

	f := &sqliteManagerFixture{scope: testScope("sql-manager")}
	record := testRecord(f.scope, "item-a", "revision-a", "eligible text",
		memory.SensitivityStandard, memory.RetentionClassStandard)
	record.SourceIDs = []string{"source-a"}
	record.ConversationIDs = []string{"conversation-a"}

	for _, change := range changes {
		change(&record)
	}

	f.canonical = newFakeCanonical(f.scope, 7, record)
	f.privacy = &sqliteFixturePrivacy{canonical: f.canonical, revisions: make(map[string]memory.MemoryRevision), sources: make(map[string]bool)}
	manifest := activeManifest(f.scope, "sqlite-generation-7", 7)
	manifest.ProjectionID = "sqlite"
	f.checkpoints = &sqliteManagerCheckpoints{fakeManifestSource: &fakeManifestSource{manifest: manifest}}

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	store, err := sqlitestore.Open(t.Context(), dir, sqlitestore.Options{EmbeddingsEnabled: true})
	if err != nil {
		t.Fatal(err)
	}

	f.store = &sqliteManagerStore{Store: store}

	t.Cleanup(func() { _ = store.Close() })

	f.db, err = sql.Open("sqlite", filepath.Join(dir, "search", "search.sqlite")+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = f.db.Close() })

	applier, err := sqlitestore.NewSQLiteCanonicalApplier(store, f.canonical, f.privacy)
	if err != nil {
		t.Fatal(err)
	}

	applier.ConfigureProjectionRetention(f.checkpoints, "sqlite")

	if err := applier.BeginProjectionRebuild(t.Context(), f.scope, manifest.GenerationID); err != nil {
		t.Fatal(err)
	}

	if err := applier.Apply(t.Context(), f.scope, manifest.GenerationID, nil,
		[]memory.MemoryRevision{{RevisionID: record.RevisionID}}); err != nil {
		t.Fatal(err)
	}

	if err := applier.CommitProjectionManifest(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}

	if err := applier.Activate(t.Context(), f.scope, manifest.GenerationID); err != nil {
		t.Fatal(err)
	}

	f.manager, err = New(f.dependencies())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = f.manager.Close(context.Background()) })

	return f
}

func (f *sqliteManagerFixture) dependencies() Dependencies {
	return Dependencies{Canonical: f.canonical, Records: f.canonical,
		Manifests: f.checkpoints, Physical: f.store, Provider: &fakeProvider{vector: []float32{1, 0}},
		Store: f.store, Lifecycle: f.store, LexicalProjectionID: "sqlite", ModelSpace: testModelSpace("model-a")}
}

func (f *sqliteManagerFixture) exec(t *testing.T, statement string) {
	t.Helper()

	if _, err := f.db.ExecContext(t.Context(), statement); err != nil {
		t.Fatal(err)
	}
}

func (f *sqliteManagerFixture) assertFamilies(t *testing.T, build string, controls, coverage, chunks int) {
	t.Helper()

	for _, check := range []struct {
		table string
		want  int
	}{
		{"semantic_build", controls}, {"semantic_coverage", coverage}, {"semantic_chunk", chunks},
		{"semantic_source", chunks}, {"semantic_conversation", chunks},
	} {
		query := "SELECT count(*) FROM " + check.table

		var args []any

		if build != "" {
			if check.table == "semantic_source" || check.table == "semantic_conversation" {
				query += " WHERE chunk_rowid IN (SELECT rowid FROM semantic_chunk WHERE build_id=?)"
			} else {
				query += " WHERE build_id=?"
			}

			args = []any{build}
		}

		var got int
		if err := f.db.QueryRowContext(t.Context(), query, args...).Scan(&got); err != nil || got != check.want {
			t.Fatalf("%s count=%d want=%d err=%v", check.table, got, check.want, err)
		}
	}
}
