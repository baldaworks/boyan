//go:build integration

//nolint:cyclop,funlen,gocognit,lll,nestif,testpackage // Real SQL startup matrices retain explicit ownership and lifecycle assertions.
package semanticprojection

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
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

// Runtime GC deliberately protects complete pending rows; startup repair must
// remove them after all producer owners have ended, while preserving active data.
func TestSQLiteStartupRepairsQuiescentOrphanAndPreservesActive(t *testing.T) {
	f := newSQLiteManagerFixture(t)
	if err := f.manager.Repair(t.Context(), f.scope); err != nil {
		t.Fatal(err)
	}

	active, found, err := f.store.ActiveState(t.Context(), f.scope)
	if err != nil || !found {
		t.Fatal(err)
	}

	f.stageCopy(t, "abandoned-before-start", f.manager.spaceID)
	f.assertFamilies(t, "abandoned-before-start", 1, 1, 1)
	lifecycle := &startupLifecycle{CandidateLifecycle: f.store}

	f.manager.lifecycle = lifecycle
	if err := f.manager.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	f.assertFamilies(t, "abandoned-before-start", 0, 0, 0)
	f.assertFamilies(t, active.BuildID, 1, 1, 1)

	if lifecycle.calls.Load() != 1 {
		t.Fatal("startup did not perform one exclusive orphan repair")
	}
}

// Without one gate, Start either sweeps a paused complete candidate or returns
// before a pre-Start producer/its exact terminal guard has relinquished ownership.
func TestSQLiteStartupWaitsForAlreadyAdmittedProducer(t *testing.T) {
	for _, boundary := range []string{"provider", "partial-stage", "complete-stage", "terminal-guard"} {
		t.Run(boundary, func(t *testing.T) {
			paused, resume := make(chan struct{}), make(chan struct{})

			unblock := sync.OnceFunc(func() { close(resume) })
			defer unblock()

			name := startupSQLFunction(t, func() { close(paused); <-resume })
			f := newSQLiteManagerFixture(t)
			lifecycle := &startupLifecycle{CandidateLifecycle: f.store}
			f.manager.lifecycle = lifecycle
			elected := make(chan struct{})
			f.manager.initializationElected = func() { close(elected) }

			var buildCtx context.Context

			f.manager.beforeBuild = func(ctx context.Context, _ memory.Scope) { buildCtx = ctx }

			switch boundary {
			case "provider":
				f.manager.provider = startupProvider{embed: func(context.Context, []string) ([][]float32, error) {
					close(paused)
					<-resume

					return [][]float32{{1, 0}}, nil
				}}
			case "partial-stage":
				f.exec(t, "CREATE TRIGGER pause_stage BEFORE INSERT ON semantic_chunk BEGIN SELECT "+name+"(); END")
			case "complete-stage":
				f.manager.beforePublish = func() { close(paused); <-resume }
			case "terminal-guard":
				f.manager.beforeFinalCheck = func() { f.canonical.setSequence(8) }
				f.store.release = func(ctx context.Context, scope memory.Scope, build string) (semanticcore.CandidateRelease, error) {
					close(paused)
					<-resume

					return f.store.Store.ReleaseUnpublished(ctx, scope, build)
				}
			}

			repaired, started := make(chan error, 1), make(chan error, 1)
			go func() { repaired <- f.manager.Repair(t.Context(), f.scope) }()

			<-paused

			go func() { started <- f.manager.Start(t.Context()) }()

			select {
			case <-elected:
			case err := <-started:
				t.Fatalf("Start crossed admitted %s producer: %v", boundary, err)
			}

			if lifecycle.calls.Load() != 0 || buildCtx.Err() != nil {
				t.Fatal("startup repair crossed admitted owner")
			}
			// These metadata operations need lifecycleMu, so success also proves
			// the initializer holds no lifecycle lock during producer drain.
			if err := f.manager.Schedule(f.scope); !errors.Is(err, ErrNotStarted) {
				t.Fatalf("Schedule=%v", err)
			}

			if got := f.manager.OperationalState(); got != StateRepairing {
				t.Fatalf("initializing status=%s", got)
			}

			assertStartupLexicalReady(t, f)

			unblock()

			repairErr := <-repaired
			if boundary == "terminal-guard" {
				if !errors.Is(repairErr, ErrStaleView) {
					t.Fatalf("Repair=%v", repairErr)
				}
			} else if repairErr != nil {
				t.Fatal(repairErr)
			}

			if err := <-started; err != nil {
				t.Fatal(err)
			}

			if lifecycle.calls.Load() != 1 {
				t.Fatal("duplicate or missing startup repair")
			}

			want := 1
			if boundary == "terminal-guard" {
				want = 0
			}

			f.assertFamilies(t, "", want, want, want)
		})
	}
}

func TestSQLiteStartupWaitersShareAttemptAndDoNotCancelInitializer(t *testing.T) {
	f := newSQLiteManagerFixture(t)
	seedStartupOrphan(t, f)

	entered, resume := make(chan struct{}), make(chan struct{})

	unblock := sync.OnceFunc(func() { close(resume) })
	defer unblock()

	lifecycle := &startupLifecycle{CandidateLifecycle: f.store}
	lifecycle.repair = func(ctx context.Context) error {
		close(entered)

		select {
		case <-resume:
		case <-ctx.Done():
			return ctx.Err()
		}

		return f.store.RepairSemanticOrphans(ctx)
	}
	f.manager.lifecycle = lifecycle
	joined := make(chan struct{}, 4)
	f.manager.joinedInitialization = func() { joined <- struct{}{} }

	started, duplicate, repaired := make(chan error, 1), make(chan error, 1), make(chan error, 1)
	go func() { started <- f.manager.Start(t.Context()) }()

	<-entered

	go func() { duplicate <- f.manager.Start(t.Context()) }()

	<-joined

	go func() { repaired <- f.manager.Repair(t.Context(), f.scope) }()

	<-joined

	for _, start := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		result := make(chan error, 1)

		go func() {
			if start {
				result <- f.manager.Start(ctx)
			} else {
				result <- f.manager.Repair(ctx, f.scope)
			}
		}()

		<-joined
		cancel()

		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatalf("waiter=%v", err)
		}
	}

	if lifecycle.calls.Load() != 1 {
		t.Fatal("duplicate initializer")
	}

	f.assertFamilies(t, "startup-orphan", 1, 1, 1)
	unblock()

	for _, result := range []<-chan error{started, duplicate, repaired} {
		if err := <-result; err != nil {
			t.Fatal(err)
		}
	}

	f.assertFamilies(t, "startup-orphan", 0, 0, 0)

	if err := f.manager.Start(t.Context()); err != nil || lifecycle.calls.Load() != 1 {
		t.Fatalf("repeat Start=%v", err)
	}
	// Two different scheduled scopes enter concurrently before source access.
	workers := make(chan struct{}, 2)

	f.manager.beforeBuild = func(ctx context.Context, _ memory.Scope) { workers <- struct{}{}; <-ctx.Done() }
	if err := f.manager.Schedule(f.scope); err != nil {
		t.Fatal(err)
	}

	if err := f.manager.Schedule(testScope("second-worker")); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		<-workers
	}

	if len(f.manager.buildSlots) != 2 {
		t.Fatal("expected exactly two admitted worker builds")
	}

	if err := f.manager.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteStartupFailureWakesWaitersAndAllowsRepairAndRetry(t *testing.T) {
	f := newSQLiteManagerFixture(t)
	seedStartupOrphan(t, f)
	f.manager.recordCleanupDeferred(f.scope)
	f.manager.recordBuildFailure(f.scope, buildTarget{}, ErrStaleView)
	f.exec(t, "CREATE TRIGGER fail_startup BEFORE DELETE ON semantic_chunk BEGIN SELECT RAISE(ABORT,'private startup SQL'); END")

	entered, resume := make(chan struct{}), make(chan struct{})

	unblock := sync.OnceFunc(func() { close(resume) })
	defer unblock()

	lifecycle := &startupLifecycle{CandidateLifecycle: f.store}
	lifecycle.repair = func(ctx context.Context) error {
		close(entered)
		<-resume

		return f.store.RepairSemanticOrphans(ctx)
	}
	f.manager.lifecycle = lifecycle
	joined := make(chan struct{}, 2)
	f.manager.joinedInitialization = func() { joined <- struct{}{} }

	started, duplicate, repaired := make(chan error, 1), make(chan error, 1), make(chan error, 1)
	go func() { started <- f.manager.Start(t.Context()) }()

	<-entered

	go func() { duplicate <- f.manager.Start(t.Context()) }()

	<-joined

	go func() { repaired <- f.manager.Repair(t.Context(), f.scope) }()

	<-joined
	unblock()

	failure := <-started
	if failure == nil {
		t.Fatal("startup failure ignored")
	}

	for _, result := range []<-chan error{duplicate, repaired} {
		if err := <-result; err != failure {
			t.Fatalf("waiter bypassed initialization failure: %v vs %v", err, failure)
		}
	}

	status, err := f.manager.Status(t.Context(), f.scope)
	if err != nil || status.State != StateDegraded || !status.CleanupDeferred || status.SafeReason != semanticcore.ReasonUnavailable ||
		f.manager.OperationalState() != StateDegraded {
		t.Fatalf("failure status=%#v err=%v", status, err)
	}

	if err := f.manager.Schedule(f.scope); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("failed startup launched workers: %v", err)
	}

	assertStartupLexicalReady(t, f)
	// Direct Repair remains valid after a failed attempt; active data is intact.
	if err := f.manager.Repair(t.Context(), f.scope); err != nil {
		t.Fatal(err)
	}

	f.assertFamilies(t, "startup-orphan", 1, 1, 1)
	f.exec(t, "DROP TRIGGER fail_startup")

	lifecycle.repair = nil

	if err := f.manager.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	f.assertFamilies(t, "startup-orphan", 0, 0, 0)

	status, err = f.manager.Status(t.Context(), f.scope)
	if err != nil || status.CleanupDeferred || status.CleanupReason != "" || status.State != StateReady {
		t.Fatalf("recovery status=%#v %v", status, err)
	}

	if lifecycle.calls.Load() != 2 {
		t.Fatal("retry did not elect exactly one new initializer")
	}
}

func TestSQLiteStartupParentCancellationReopensAdmission(t *testing.T) {
	f := newSQLiteManagerFixture(t)
	paused, resume := make(chan struct{}), make(chan struct{})

	unblock := sync.OnceFunc(func() { close(resume) })
	defer unblock()

	f.manager.provider = startupProvider{embed: func(context.Context, []string) ([][]float32, error) {
		close(paused)
		<-resume

		return [][]float32{{1, 0}}, nil
	}}

	producer := make(chan error, 1)
	go func() { producer <- f.manager.Repair(t.Context(), f.scope) }()

	<-paused

	elected, joined := make(chan struct{}), make(chan struct{})
	f.manager.initializationElected = func() { close(elected) }
	f.manager.joinedInitialization = func() { close(joined) }

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	started, waitingRepair := make(chan error, 1), make(chan error, 1)
	go func() { started <- f.manager.Start(ctx) }()

	<-elected

	go func() { waitingRepair <- f.manager.Repair(t.Context(), f.scope) }()

	<-joined
	cancel()

	if err := <-started; !errors.Is(err, context.Canceled) {
		t.Fatalf("initializer=%v", err)
	}

	if err := <-waitingRepair; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting Repair bypassed cancellation=%v", err)
	}

	unblock()

	if err := <-producer; err != nil {
		t.Fatalf("initializer canceled independent producer: %v", err)
	}

	if err := f.manager.Repair(t.Context(), f.scope); err != nil {
		t.Fatal(err)
	}

	f.manager.initializationElected = nil
	if err := f.manager.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteStartupCountsOnlySingleFlightProducer(t *testing.T) {
	f := newSQLiteManagerFixture(t)
	paused, resume, joinedFlight, elected, joinedInit := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})

	unblock := sync.OnceFunc(func() { close(resume) })
	defer unblock()

	f.manager.beforePublish = func() { close(paused); <-resume }
	f.manager.joinedFlight = func() { close(joinedFlight) }
	f.manager.initializationElected = func() { close(elected) }
	f.manager.joinedInitialization = func() { close(joinedInit) }

	owner, joiner, start, waiting := make(chan error, 1), make(chan error, 1), make(chan error, 1), make(chan error, 1)
	go func() { owner <- f.manager.Repair(t.Context(), f.scope) }()

	<-paused

	go func() { joiner <- f.manager.Repair(t.Context(), f.scope) }()

	<-joinedFlight
	f.manager.lifecycleMu.Lock()
	count := f.manager.producerCount
	f.manager.lifecycleMu.Unlock()

	if count != 1 {
		t.Fatalf("single flight enrolled %d producers", count)
	}

	go func() { start <- f.manager.Start(t.Context()) }()

	<-elected

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() { waiting <- f.manager.Repair(ctx, f.scope) }()

	<-joinedInit
	cancel()

	if err := <-waiting; !errors.Is(err, context.Canceled) {
		t.Fatalf("late join bypassed startup gate: %v", err)
	}

	unblock()

	for _, result := range []<-chan error{owner, joiner, start} {
		if err := <-result; err != nil {
			t.Fatal(err)
		}
	}

	f.manager.lifecycleMu.Lock()
	count = f.manager.producerCount
	f.manager.lifecycleMu.Unlock()

	if count != 0 {
		t.Fatalf("producer count leaked=%d", count)
	}

	f.assertFamilies(t, "", 1, 1, 1)
}

func TestSQLiteStartupStopCancelsAndJoinsInitializer(t *testing.T) {
	for _, boundary := range []string{"drain", "repair", "before-worker-commit"} {
		t.Run(boundary, func(t *testing.T) {
			paused, resume := make(chan struct{}), make(chan struct{})

			unblock := sync.OnceFunc(func() { close(resume) })
			defer unblock()

			name := startupSQLFunction(t, func() { close(paused); <-resume })
			f := newSQLiteManagerFixture(t)

			started, repaired := make(chan error, 1), make(chan error, 1)
			if boundary == "drain" {
				elected, guarding := make(chan struct{}), make(chan struct{})
				f.manager.initializationElected = func() { close(elected) }

				var buildCtx context.Context

				f.manager.beforeBuild = func(ctx context.Context, _ memory.Scope) { buildCtx = ctx }
				f.manager.beforePublish = func() { close(paused); <-buildCtx.Done() }

				f.store.release = func(ctx context.Context, scope memory.Scope, build string) (semanticcore.CandidateRelease, error) {
					close(guarding)
					<-resume

					return f.store.Store.ReleaseUnpublished(ctx, scope, build)
				}
				go func() { repaired <- f.manager.Repair(t.Context(), f.scope) }()

				<-paused

				go func() { started <- f.manager.Start(t.Context()) }()

				<-elected

				done := f.manager.stop()

				<-guarding

				select {
				case <-done:
					t.Fatal("Stop crossed terminal guard")
				default:
				}

				unblock()

				if err := <-repaired; !errors.Is(err, context.Canceled) {
					t.Fatalf("producer=%v", err)
				}

				<-done
				f.assertFamilies(t, "", 0, 0, 0)
			} else {
				seedStartupOrphan(t, f)

				ctxEntered := make(chan context.Context, 1)
				lifecycle := &startupLifecycle{CandidateLifecycle: f.store}
				lifecycle.repair = func(ctx context.Context) error {
					ctxEntered <- ctx

					return f.store.RepairSemanticOrphans(ctx)
				}

				f.manager.lifecycle = lifecycle
				if boundary == "repair" {
					f.exec(t, "CREATE TRIGGER pause_startup BEFORE DELETE ON semantic_chunk BEGIN SELECT "+name+"(); END")
				} else {
					f.manager.beforeStartCommit = func() { close(paused); <-resume }
				}

				go func() { started <- f.manager.Start(t.Context()) }()

				<-paused

				maintenance := <-ctxEntered
				done := f.manager.stop()

				<-maintenance.Done()

				select {
				case <-done:
					t.Fatal("Stop crossed initializer")
				default:
				}

				unblock()
				<-done

				want := 1
				if boundary == "before-worker-commit" {
					want = 0
				}

				f.assertFamilies(t, "startup-orphan", want, want, want)
			}

			if err := <-started; !errors.Is(err, ErrClosed) {
				t.Fatalf("stopped initializer=%v", err)
			}

			if err := f.manager.Start(t.Context()); !errors.Is(err, ErrClosed) {
				t.Fatalf("startup resurrected=%v", err)
			}

			if err := f.manager.Schedule(f.scope); !errors.Is(err, ErrClosed) {
				t.Fatalf("worker resurrected=%v", err)
			}

			if _, _, err := f.store.ActiveState(t.Context(), f.scope); err != nil {
				t.Fatalf("store closed before Manager drain: %v", err)
			}

			if err := f.store.Store.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSQLiteStartupClosedPrecedesWaiterCancellation(t *testing.T) {
	f := newSQLiteManagerFixture(t)
	entered, joinEntered, resumeJoin := make(chan struct{}), make(chan struct{}), make(chan struct{})

	unblock := sync.OnceFunc(func() { close(resumeJoin) })
	defer unblock()

	lifecycle := &startupLifecycle{CandidateLifecycle: f.store}
	lifecycle.repair = func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()

		return ctx.Err()
	}
	f.manager.lifecycle = lifecycle
	f.manager.joinedInitialization = func() { close(joinEntered); <-resumeJoin }

	started, waiter := make(chan error, 1), make(chan error, 1)
	go func() { started <- f.manager.Start(t.Context()) }()

	<-entered

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() { waiter <- f.manager.Repair(ctx, f.scope) }()

	<-joinEntered
	cancel()

	if err := f.manager.Close(t.Context()); err != nil {
		t.Fatal(err)
	}

	unblock()

	if err := <-waiter; !errors.Is(err, ErrClosed) {
		t.Fatalf("waiter precedence=%v", err)
	}

	if err := <-started; !errors.Is(err, ErrClosed) {
		t.Fatalf("initializer precedence=%v", err)
	}

	if err := f.manager.Start(ctx); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed Start precedence=%v", err)
	}
}

func TestSQLiteStartupDeadlineCancelsActualSQLAndAllowsRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	entered, resume := make(chan struct{}), make(chan struct{})

	unblock := sync.OnceFunc(func() { close(resume) })
	defer unblock()

	name := startupSQLFunction(t, func() { close(entered); <-ctx.Done(); <-resume })
	f := newSQLiteManagerFixture(t)
	seedStartupOrphan(t, f)
	f.exec(t, "CREATE TRIGGER deadline_startup BEFORE DELETE ON semantic_chunk BEGIN SELECT "+name+"(); END")

	deadline, cancelDeadline := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancelDeadline()

	started := make(chan error, 1)
	go func() { started <- f.manager.Start(deadline) }()

	<-entered
	<-deadline.Done()
	cancel()
	unblock()

	if err := <-started; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("SQL deadline identity=%v", err)
	}

	f.assertFamilies(t, "startup-orphan", 1, 1, 1)

	if f.manager.OperationalState() != StateDegraded {
		t.Fatal("deadline failure not visible")
	}

	f.exec(t, "DROP TRIGGER deadline_startup")

	if err := f.manager.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	f.assertFamilies(t, "startup-orphan", 0, 0, 0)
}

func TestSQLiteStartupFailureSanitizesProseAndPreservesClassification(t *testing.T) {
	for _, classified := range []bool{false, true} {
		t.Run(fmt.Sprint(classified), func(t *testing.T) {
			f := newSQLiteManagerFixture(t)
			seedStartupOrphan(t, f)

			primary := errors.New("private owner identity and provider prose")

			var failure error = primary

			wantCode, wantClass := memory.CodeStoreFailure, memory.ErrorClassPermanent

			if classified {
				failure = memory.RetryableError(memory.CodeConflict, "private SQL", primary)
				wantCode, wantClass = memory.CodeConflict, memory.ErrorClassRetryable
			}

			lifecycle := &startupLifecycle{CandidateLifecycle: f.store}
			lifecycle.repair = func(ctx context.Context) error {
				if err := f.store.RepairSemanticOrphans(ctx); err != nil {
					return err
				}

				return failure
			}
			f.manager.lifecycle = lifecycle
			err := f.manager.Start(t.Context())

			code, class, ok := memory.ClassifyError(err)
			if !errors.Is(err, primary) || strings.Contains(err.Error(), "private") || !ok || code != wantCode || class != wantClass {
				t.Fatalf("unsafe or unclassified initialization: %v %v %v %v", err, code, class, ok)
			}

			status, statusErr := f.manager.Status(t.Context(), f.scope)
			if statusErr != nil || status.State != StateDegraded || status.SafeReason != semanticcore.ReasonUnavailable {
				t.Fatalf("startup status=%#v %v", status, statusErr)
			}

			f.assertFamilies(t, "startup-orphan", 0, 0, 0)
		})
	}
}

func TestSQLiteStartupReopenRecoversDeferredAbandonedCandidate(t *testing.T) {
	f := newSQLiteManagerFixture(t)
	seedStartupOrphan(t, f)

	if err := f.manager.Close(t.Context()); err != nil {
		t.Fatal(err)
	}

	var (
		sequence   int
		name, file string
	)
	if err := f.db.QueryRowContext(t.Context(), "PRAGMA database_list").Scan(&sequence, &name, &file); err != nil {
		t.Fatal(err)
	}

	if err := f.store.Store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := sqlitestore.Open(t.Context(), filepath.Dir(filepath.Dir(file)), sqlitestore.Options{EmbeddingsEnabled: true})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	applier, err := sqlitestore.NewSQLiteCanonicalApplier(store, f.canonical, f.privacy)
	if err != nil {
		t.Fatal(err)
	}

	applier.ConfigureProjectionRetention(f.checkpoints, "sqlite")
	deps := f.dependencies()
	deps.Store, deps.Lifecycle, deps.Physical = store, store, store

	manager, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = manager.Close(context.Background()) })

	if err := manager.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	f.assertFamilies(t, "startup-orphan", 0, 0, 0)

	if err := manager.Repair(t.Context(), f.scope); err != nil {
		t.Fatal(err)
	}

	if status, err := manager.Status(t.Context(), f.scope); err != nil || status.State != StateReady {
		t.Fatalf("reopen status=%#v %v", status, err)
	}
}

func TestSQLiteStartupStopBeforeElectionPreservesOrphan(t *testing.T) {
	f := newSQLiteManagerFixture(t)
	seedStartupOrphan(t, f)
	lifecycle := &startupLifecycle{CandidateLifecycle: f.store}

	f.manager.lifecycle = lifecycle
	if err := f.manager.Close(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := f.manager.Start(t.Context()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Start=%v", err)
	}

	if lifecycle.calls.Load() != 0 {
		t.Fatal("Stop ran global orphan cleanup")
	}

	f.assertFamilies(t, "startup-orphan", 1, 1, 1)
}

func TestSQLiteStartupParentCancelAfterRepairPreventsWorkerCommit(t *testing.T) {
	f := newSQLiteManagerFixture(t)
	seedStartupOrphan(t, f)

	paused, resume := make(chan struct{}), make(chan struct{})

	unblock := sync.OnceFunc(func() { close(resume) })
	defer unblock()

	f.manager.beforeStartCommit = func() { close(paused); <-resume }

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	started := make(chan error, 1)
	go func() { started <- f.manager.Start(ctx) }()

	<-paused
	f.assertFamilies(t, "startup-orphan", 0, 0, 0)
	cancel()
	unblock()

	if err := <-started; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled commit=%v", err)
	}

	if err := f.manager.Schedule(f.scope); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("canceled initializer started workers=%v", err)
	}

	if err := f.manager.Repair(t.Context(), f.scope); err != nil {
		t.Fatal(err)
	}

	f.manager.beforeStartCommit = nil
	if err := f.manager.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func seedStartupOrphan(t *testing.T, f *sqliteManagerFixture) {
	t.Helper()

	if err := f.manager.Repair(t.Context(), f.scope); err != nil {
		t.Fatal(err)
	}

	f.stageCopy(t, "startup-orphan", f.manager.spaceID)
}

func assertStartupLexicalReady(t *testing.T, f *sqliteManagerFixture) {
	t.Helper()

	at := time.Unix(100, 0).UTC()

	result, err := f.store.SearchRecallCertified(t.Context(), memory.RecallRequest{Scope: f.scope,
		Query: "eligible", Limit: 1, MinScopeChangeSeq: 7, AsOf: &at}, 10)
	if err != nil || !result.Certified || len(result.Hits) != 1 || result.Hits[0].RevisionID != "revision-a" {
		t.Fatalf("semantic initialization blocked lexical readiness: %#v %v", result, err)
	}
}

func startupSQLFunction(t *testing.T, action func()) string {
	t.Helper()

	name := fmt.Sprintf("startup_gate_%d", managerSQLFunctionSequence.Add(1))
	if err := sqlitedriver.RegisterScalarFunction(name, 0,
		func(*sqlitedriver.FunctionContext, []driver.Value) (driver.Value, error) {
			action()

			return int64(0), nil
		}); err != nil {
		t.Fatal(err)
	}

	return name
}

type startupProvider struct {
	embed func(context.Context, []string) ([][]float32, error)
}

func (p startupProvider) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	return p.embed(ctx, inputs)
}

type startupLifecycle struct {
	CandidateLifecycle

	repair func(context.Context) error
	calls  atomic.Int32
}

func (l *startupLifecycle) RepairSemanticOrphans(ctx context.Context) error {
	l.calls.Add(1)

	if l.repair != nil {
		return l.repair(ctx)
	}

	return l.CandidateLifecycle.RepairSemanticOrphans(ctx)
}

var _ CandidateLifecycle = (*startupLifecycle)(nil)
