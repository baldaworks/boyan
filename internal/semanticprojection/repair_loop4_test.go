//go:build integration

package semanticprojection //nolint:testpackage // Exercises deterministic private scheduler seams.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	semanticstore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

//nolint:cyclop,funlen,wsl_v5 // Every synchronization point establishes the saturated scheduler regression.
func TestAcceptedInFlightEpochCompletesWhenQueueRefillsToCapacity(t *testing.T) {
	scope := testScope("accepted-in-flight-epoch")
	blocker := testScope("worker-blocker")
	record := testRecord(
		scope, "item-a", "revision-a", "text",
		memory.SensitivityStandard, memory.RetentionClassStandard,
	)
	canonical := newFakeCanonical(scope, 7, record)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "sqlite-generation-7", 7)}
	provider := newGenerationBlockingProvider()
	store := openSemanticStore(t, canonical)
	manager, err := New(Dependencies{
		Canonical: canonical, Records: canonical, Manifests: manifests, Physical: manifests,
		Provider: provider, Store: store, Lifecycle: store, ModelSpace: testModelSpace("model-a"),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if closeErr := manager.Close(ctx); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})

	blockerEntered := make(chan struct{})
	releaseBlocker := make(chan struct{})
	var blockerOnce sync.Once
	manager.beforeBuild = func(ctx context.Context, got memory.Scope) {
		if got != blocker {
			return
		}
		blockerOnce.Do(func() { close(blockerEntered) })
		select {
		case <-releaseBlocker:
		case <-ctx.Done():
		}
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	repairDone := make(chan error, 1)
	go func() { repairDone <- manager.Repair(context.Background(), scope) }()
	waitClosed(t, provider.firstEntered, "W7 provider call was not entered")

	if err := manager.Schedule(blocker); err != nil {
		t.Fatalf("Schedule(blocker) error = %v", err)
	}
	waitClosed(t, blockerEntered, "second worker did not enter blocker")

	joined := make(chan struct{})
	var joinedOnce sync.Once
	manager.joinedFlight = func() { joinedOnce.Do(func() { close(joined) }) }
	canonical.replace(8, record)
	manifests.set(activeManifest(scope, "sqlite-generation-8", 8))
	if err := manager.Schedule(scope); err != nil {
		t.Fatalf("Schedule(W8) error = %v", err)
	}
	waitClosed(t, joined, "scheduled W8 request did not join W7 flight")

	for index := range semanticcore.SemanticQueueLimit {
		queued := testScope(fmt.Sprintf("capacity-refill-%03d", index))
		if err := manager.Schedule(queued); err != nil {
			t.Fatalf("Schedule(queue index %d) error = %v", index, err)
		}
	}
	if err := manager.Schedule(testScope("capacity-overflow")); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("Schedule(one over queue) error = %v, want ErrQueueFull", err)
	}

	close(provider.releaseFirst)
	if err := <-repairDone; !errors.Is(err, ErrStaleView) {
		t.Fatalf("Repair(W7) error = %v, want stale view", err)
	}
	waitClosed(t, provider.secondEntered, "accepted W8 request was dropped after W7 completion")

	state := waitActiveState(t, store, scope, func(state semanticstore.State, found bool) bool {
		return found && state.Status == semanticstore.StatusReady &&
			state.LexicalGenerationID == "sqlite-generation-8" && state.LexicalWatermark == 8
	})
	close(releaseBlocker)
	waitSchedulerQuiescent(t, manager)
	if provider.Calls() != 2 {
		t.Fatalf("provider calls = %d, want exactly W7 plus one W8 completion", provider.Calls())
	}
	if state.Status != semanticstore.StatusReady {
		t.Fatalf("ActiveState() = %#v, want W8 ready", state)
	}
}

//nolint:cyclop,funlen,wsl_v5 // Every synchronization point establishes caller-independent scheduled ownership.
func TestScheduledEpochJoinedBeforeSnapshotSurvivesExplicitRepairCancellation(t *testing.T) {
	scope := testScope("scheduled-caller-cancellation")
	record := testRecord(
		scope, "item-a", "revision-a", "text",
		memory.SensitivityStandard, memory.RetentionClassStandard,
	)
	canonical := newFakeCanonical(scope, 8, record)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "sqlite-generation-8", 8)}
	provider := &fakeProvider{vector: []float32{1, 0}}
	store := openSemanticStore(t, canonical)
	manager, err := New(Dependencies{
		Canonical: canonical, Records: canonical, Manifests: manifests, Physical: manifests,
		Provider: provider, Store: store, Lifecycle: store, ModelSpace: testModelSpace("model-a"),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })

	beforeSnapshot := make(chan struct{})
	releaseSnapshot := make(chan struct{})
	manager.beforeBuild = func(ctx context.Context, got memory.Scope) {
		if got != scope {
			return
		}
		select {
		case <-beforeSnapshot:
		case <-ctx.Done():
			return
		default:
			close(beforeSnapshot)
		}
		select {
		case <-releaseSnapshot:
		case <-ctx.Done():
		}
	}

	afterSnapshot := make(chan struct{})
	releaseCanonical := make(chan struct{})
	var loadOnce sync.Once
	canonical.loadStateHook = func(ctx context.Context) {
		loadOnce.Do(func() {
			close(afterSnapshot)
			select {
			case <-releaseCanonical:
			case <-ctx.Done():
			}
		})
	}

	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	joined := make(chan struct{})
	var joinedOnce sync.Once
	manager.joinedFlight = func() { joinedOnce.Do(func() { close(joined) }) }
	repairCtx, cancelRepair := context.WithCancel(context.Background())
	repairDone := make(chan error, 1)
	go func() { repairDone <- manager.Repair(repairCtx, scope) }()
	waitClosed(t, beforeSnapshot, "explicit repair did not reach the pre-snapshot boundary")
	if err := manager.Schedule(scope); err != nil {
		t.Fatalf("Schedule(W8) error = %v", err)
	}
	waitClosed(t, joined, "scheduled W8 request did not join explicit repair")
	close(releaseSnapshot)
	waitClosed(t, afterSnapshot, "explicit repair did not snapshot accepted W8 epoch")
	cancelRepair()
	close(releaseCanonical)

	if err := <-repairDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("Repair() error = %v, want caller cancellation", err)
	}
	state := waitActiveState(t, store, scope, func(state semanticstore.State, found bool) bool {
		return found && state.Status == semanticstore.StatusReady && state.LexicalWatermark == 8
	})
	waitSchedulerQuiescent(t, manager)
	if inputs := provider.Inputs(); len(inputs) != 1 {
		t.Fatalf("worker-context provider inputs = %#v, want one W8 attempt", inputs)
	}
	if state.LexicalGenerationID != "sqlite-generation-8" {
		t.Fatalf("ActiveState() = %#v, want worker-context W8 publication", state)
	}
}

//nolint:cyclop,funlen,wsl_v5 // Queued, in-flight, admission, and idempotent close are one lifecycle boundary.
func TestStartContextCancellationStopsAdmissionAndReconcilesScheduledScopes(t *testing.T) {
	scopeA := testScope("parent-cancel-in-flight-a")
	scopeB := testScope("parent-cancel-in-flight-b")
	queued := testScope("parent-cancel-queued")
	canonical := newFakeCanonical(scopeA, 7)
	manifests := &fakeManifestSource{manifest: activeManifest(scopeA, "sqlite-generation-7", 7)}
	store := openSemanticStore(t, canonical)
	manager, err := New(Dependencies{
		Canonical: canonical, Records: canonical, Manifests: manifests, Physical: manifests,
		Provider: &fakeProvider{vector: []float32{1, 0}}, Store: store, Lifecycle: store,
		ModelSpace: testModelSpace("model-a"),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	entered := make(chan memory.Scope, semanticcore.SemanticWorkerLimit)
	manager.beforeBuild = func(ctx context.Context, scope memory.Scope) {
		entered <- scope
		<-ctx.Done()
	}
	startCtx, cancelStart := context.WithCancel(context.Background())
	if err := manager.Start(startCtx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := manager.Schedule(scopeA); err != nil {
		t.Fatalf("Schedule(scope A) error = %v", err)
	}
	if err := manager.Schedule(scopeB); err != nil {
		t.Fatalf("Schedule(scope B) error = %v", err)
	}
	for range semanticcore.SemanticWorkerLimit {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("workers did not enter in-flight builds")
		}
	}
	if err := manager.Schedule(queued); err != nil {
		t.Fatalf("Schedule(queued) error = %v", err)
	}

	cancelStart()
	waitManagerStopped(t, manager)
	for _, scope := range []memory.Scope{scopeA, scopeB, queued} {
		status, statusErr := manager.Status(context.Background(), scope)
		if statusErr != nil || status.State != StateStopped {
			t.Fatalf("Status(%q) = (%#v, %v), want stopped", scope.Key, status, statusErr)
		}
	}
	if err := manager.Schedule(testScope("post-parent-cancel")); !errors.Is(err, ErrClosed) {
		t.Fatalf("Schedule(after parent cancellation) error = %v, want ErrClosed", err)
	}
	if err := manager.Repair(context.Background(), scopeA); !errors.Is(err, ErrClosed) {
		t.Fatalf("Repair(after parent cancellation) error = %v, want ErrClosed", err)
	}
	if err := manager.Close(context.Background()); err != nil {
		t.Fatalf("Close(after parent cancellation) error = %v", err)
	}
	if err := manager.Close(context.Background()); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

//nolint:cyclop,funlen,wsl_v5 // Ordered build gates prove bounded continuation fairness without timing assumptions.
func TestContinuousSameScopeSchedulingYieldsToCompetingScopeWithoutLatentRun(t *testing.T) {
	hot := testScope("continuous-hot")
	competing := testScope("competing")
	blocker := testScope("fairness-worker-blocker")
	canonical := newFakeCanonical(hot, 7)
	manifests := allScopeManifestSource{}
	store := openSemanticStore(t, canonical)
	manager, err := New(Dependencies{
		Canonical: canonical, Records: canonical, Manifests: manifests, Physical: manifests,
		Provider: &fakeProvider{vector: []float32{1, 0}}, Store: store, Lifecycle: store,
		ModelSpace: testModelSpace("model-a"),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })

	blockerEntered := make(chan struct{})
	releaseBlocker := make(chan struct{})
	buildStarted := make(chan memory.Scope, 4)
	var buildCount atomic.Int32
	var blockerOnce sync.Once
	manager.beforeBuild = func(ctx context.Context, scope memory.Scope) {
		buildCount.Add(1)
		if scope == blocker {
			blockerOnce.Do(func() { close(blockerEntered) })
			select {
			case <-releaseBlocker:
			case <-ctx.Done():
			}

			return
		}
		buildStarted <- scope
	}
	finalCheck := make(chan struct{}, 4)
	releaseFinalCheck := make(chan struct{})
	var releaseFinalOnce sync.Once
	releaseFinalGates := func() { releaseFinalOnce.Do(func() { close(releaseFinalCheck) }) }
	t.Cleanup(releaseFinalGates)
	manager.beforeFinalCheck = func() {
		finalCheck <- struct{}{}
		<-releaseFinalCheck
	}

	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := manager.Schedule(blocker); err != nil {
		t.Fatalf("Schedule(blocker) error = %v", err)
	}
	waitClosed(t, blockerEntered, "second worker did not enter fairness blocker")
	if err := manager.Schedule(hot); err != nil {
		t.Fatalf("Schedule(hot) error = %v", err)
	}

	waitBuildAtFinalCheck(t, buildStarted, finalCheck, hot)
	if err := manager.Schedule(competing); err != nil {
		t.Fatalf("Schedule(competing) error = %v", err)
	}
	for range 32 {
		if err := manager.Schedule(hot); err != nil {
			t.Fatalf("Schedule(hot during first run) error = %v", err)
		}
	}
	releaseFinalCheck <- struct{}{}

	waitBuildAtFinalCheck(t, buildStarted, finalCheck, hot)
	for range 32 {
		if err := manager.Schedule(hot); err != nil {
			t.Fatalf("Schedule(hot during continuation) error = %v", err)
		}
	}
	releaseFinalCheck <- struct{}{}

	waitBuildAtFinalCheck(t, buildStarted, finalCheck, competing)
	releaseFinalCheck <- struct{}{}
	waitBuildAtFinalCheck(t, buildStarted, finalCheck, hot)
	releaseFinalCheck <- struct{}{}
	releaseFinalGates()
	close(releaseBlocker)
	waitSchedulerQuiescent(t, manager)

	if got := buildCount.Load(); got != 5 {
		t.Fatalf("build count = %d, want three hot, one competing, and one blocker build", got)
	}
	if remaining := len(buildStarted); remaining != 0 {
		t.Fatalf("unexpected latent builds after global scheduler quiescence: %d", remaining)
	}
}

type allScopeManifestSource struct{}

func (allScopeManifestSource) LoadActiveProjectionManifest(
	ctx context.Context,
	scope memory.Scope,
	projectionID string,
) (memory.ProjectionManifest, bool, error) {
	if err := ctx.Err(); err != nil {
		return memory.ProjectionManifest{}, false, err
	}

	if projectionID != lexicalProjectionID {
		return memory.ProjectionManifest{}, false, errors.New("unexpected projection ID")
	}

	return activeManifest(scope, "sqlite-generation-7", 7), true, nil
}

func (source allScopeManifestSource) LoadActivePhysicalProjectionManifest(
	ctx context.Context,
	scope memory.Scope,
	projectionID string,
) (memory.ProjectionManifest, bool, error) {
	return source.LoadActiveProjectionManifest(ctx, scope, projectionID)
}

func waitBuildAtFinalCheck(
	t *testing.T,
	buildStarted <-chan memory.Scope,
	finalCheck <-chan struct{},
	want memory.Scope,
) {
	t.Helper()

	select {
	case got := <-buildStarted:
		if got != want {
			t.Fatalf("build scope = %q, want %q", got.Key, want.Key)
		}
	case <-time.After(time.Second):
		t.Fatalf("build for %q did not start", want.Key)
	}

	select {
	case <-finalCheck:
	case <-time.After(time.Second):
		t.Fatalf("build for %q did not reach final check", want.Key)
	}
}

func waitManagerStopped(t *testing.T, manager *Manager) {
	t.Helper()

	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()

	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	for {
		manager.lifecycleMu.Lock()
		closed := manager.closed
		done := manager.done
		manager.lifecycleMu.Unlock()

		if closed && done != nil {
			select {
			case <-done:
				return
			default:
			}
		}

		select {
		case <-deadline.C:
			t.Fatal("manager did not transition to stopped after Start context cancellation")
		case <-ticker.C:
		}
	}
}

func waitSchedulerQuiescent(t *testing.T, manager *Manager) {
	t.Helper()

	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()

	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	for {
		// Match runSingleFlight's lock order and take one global snapshot.
		// Workers retain their schedule while dequeued work can still run;
		// with no further admissions, all three zeros exclude a latent build.
		manager.flightMu.Lock()
		manager.lifecycleMu.Lock()
		activeSchedules := len(manager.schedules)
		activeFlights := len(manager.flights)
		queued := len(manager.queue)
		manager.lifecycleMu.Unlock()
		manager.flightMu.Unlock()

		if activeSchedules == 0 && activeFlights == 0 && queued == 0 {
			return
		}

		select {
		case <-deadline.C:
			t.Fatalf("scheduler did not quiesce: schedules=%d flights=%d queued=%d", activeSchedules, activeFlights, queued)
		case <-ticker.C:
		}
	}
}
