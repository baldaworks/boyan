// Package semanticprojection builds disposable semantic state from canonical
// active records and binds it to one exact certified lexical projection view.
package semanticprojection

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/baldaworks/boyan/internal/embedding"
	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

const (
	lexicalProjectionID     = "bleve"
	canonicalPageSize       = 128
	projectionScanPageLimit = semanticcore.ProjectionScanRecordLimit/canonicalPageSize + 1
	// One direct continuation preserves an accepted in-flight epoch without
	// letting a continuously refreshed scope monopolize its worker.
	directContinuationLimit = 1
)

var (
	ErrClosed     = errors.New("semantic projection manager is closed")
	ErrNotStarted = errors.New("semantic projection manager is not started")
	ErrQueueFull  = errors.New("semantic projection queue is full")
	ErrStaleView  = errors.New("semantic projection source view changed")
)

// Dependencies reuses the repository's canonical, checkpoint, provider, and
// disposable-store contracts. The manager never owns canonical content.
type Dependencies struct {
	Canonical  memory.CanonicalStore
	Records    memory.RecallCanonicalReader
	Manifests  memory.RecallProjectionCheckpointReader
	Physical   memory.PhysicalProjectionViewReader
	Provider   embedding.Provider
	Store      BuildStore
	ModelSpace semanticcore.ModelSpace
}

// State is a bounded per-scope operational state.
type State string

const (
	StateRepairing State = "repairing"
	StateReady     State = "ready"
	StateDegraded  State = "degraded"
	StateStopped   State = "stopped"
)

// ScopeStatus contains only bounded, text-free diagnostic data.
type ScopeStatus struct {
	State               State
	SafeReason          semanticcore.Reason
	LexicalGenerationID string
	LexicalWatermark    uint64
	ModelSpace          string
	UpdatedAt           time.Time
}

type flight struct {
	done           chan struct{}
	err            error
	snapshotEpoch  uint64
	ownerScheduled bool
	callerCanceled bool
}

type flightResult struct {
	completedEpoch uint64
	err            error
	retryScheduled bool
}

// Manager owns bounded build concurrency and lifecycle state. It does not own
// or close the semantic store supplied by the caller.
type Manager struct {
	canonical memory.CanonicalStore
	records   memory.RecallCanonicalReader
	manifests memory.RecallProjectionCheckpointReader
	physical  memory.PhysicalProjectionViewReader
	provider  embedding.Provider
	store     BuildStore
	space     semanticcore.ModelSpace
	spaceID   string

	buildSlots   chan struct{}
	buildTimeout time.Duration
	now          func() time.Time

	flightMu     sync.Mutex
	flights      map[string]*flight
	requestMu    sync.Mutex
	requestEpoch uint64

	statusMu       sync.RWMutex
	statuses       map[string]ScopeStatus
	repairingCount uint64
	degradedCount  uint64

	lifecycleMu sync.Mutex
	started     bool
	closed      bool
	cancel      context.CancelFunc
	shutdown    context.Context
	stopAll     context.CancelFunc
	done        chan struct{}
	queue       chan memory.Scope
	schedules   map[string]*scheduleState
	workers     sync.WaitGroup
	builds      sync.WaitGroup

	beforeBuild      func(context.Context, memory.Scope)
	beforeFinalCheck func()
	beforePublish    func()
	afterPublish     func()
	joinedFlight     func()
}

// Start removes unreachable staging state, then starts exactly two background
// workers. Startup cleanup is completed before any new build can run.
//
//nolint:nlreturn,wsl_v5 // Startup cleanup and worker activation must remain visibly ordered.
func (manager *Manager) Start(parent context.Context) error {
	if manager == nil || parent == nil {
		return fmt.Errorf("semantic projection manager context is required")
	}

	manager.lifecycleMu.Lock()
	if manager.closed {
		manager.lifecycleMu.Unlock()
		return ErrClosed
	}
	if manager.started {
		manager.lifecycleMu.Unlock()
		return nil
	}
	manager.lifecycleMu.Unlock()

	if err := manager.store.CollectGarbage(parent); err != nil {
		return fmt.Errorf("clean semantic projection staging state: %w", err)
	}

	manager.lifecycleMu.Lock()
	defer manager.lifecycleMu.Unlock()
	if manager.closed {
		return ErrClosed
	}
	if manager.started {
		return nil
	}

	workerContext, cancel := context.WithCancel(parent)
	manager.cancel = cancel
	manager.started = true
	manager.workers.Add(semanticcore.SemanticWorkerLimit)
	for range semanticcore.SemanticWorkerLimit {
		go manager.worker(workerContext)
	}
	go func() {
		<-workerContext.Done()
		manager.stop()
	}()

	return nil
}

// Schedule non-blockingly coalesces one scope. Queue saturation is classified
// as capacity degradation and never affects the lexical caller's state.
//
//nolint:nlreturn,wsl_v5 // Admission and coalescing transitions remain explicit.
func (manager *Manager) Schedule(scope memory.Scope) error {
	if manager == nil {
		return ErrNotStarted
	}
	if err := scope.Validate(); err != nil {
		return err
	}

	manager.lifecycleMu.Lock()
	defer manager.lifecycleMu.Unlock()
	if manager.closed {
		return ErrClosed
	}
	if !manager.started {
		return ErrNotStarted
	}

	key := scopeKey(scope)
	state := manager.schedules[key]
	if state != nil {
		state.requestedEpoch = manager.nextRequestEpoch()
		return nil
	}

	state = &scheduleState{scope: scope, queued: true, requestedEpoch: manager.nextRequestEpoch()}
	select {
	case manager.queue <- scope:
		manager.schedules[key] = state
		manager.setStatus(scope, ScopeStatus{
			State: StateRepairing, ModelSpace: manager.spaceID, UpdatedAt: manager.now().UTC(),
		})
		return nil
	default:
		manager.setStatus(scope, ScopeStatus{
			State: StateDegraded, SafeReason: semanticcore.ReasonProjectionCapacity,
			ModelSpace: manager.spaceID, UpdatedAt: time.Now().UTC(),
		})
		return ErrQueueFull
	}
}

//nolint:funcorder,wsl_v5 // Worker mechanics stay beside scheduling while public lifecycle methods remain grouped.
func (manager *Manager) worker(ctx context.Context) {
	defer manager.workers.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case scope := <-manager.queue:
			if ctx.Err() != nil {
				return
			}
			manager.runScheduled(ctx, scope)
		}
	}
}

//nolint:cyclop,funcorder,nlreturn,wsl_v5 // Scheduler state transitions are intentionally explicit.
func (manager *Manager) runScheduled(ctx context.Context, scope memory.Scope) {
	continuing := false
	directContinuations := 0
	for {
		key := scopeKey(scope)
		manager.lifecycleMu.Lock()
		state := manager.schedules[key]
		if state == nil || (!continuing && !state.queued) {
			manager.lifecycleMu.Unlock()
			return
		}
		state.queued = false
		state.running = true
		manager.lifecycleMu.Unlock()

		result := manager.runSingleFlight(ctx, scope, true)

		manager.lifecycleMu.Lock()
		state = manager.schedules[key]
		if state == nil {
			manager.lifecycleMu.Unlock()
			return
		}
		if ctx.Err() != nil || manager.closed {
			state.running = false
			delete(manager.schedules, key)
			manager.lifecycleMu.Unlock()
			return
		}
		if !result.retryScheduled && state.requestedEpoch <= result.completedEpoch {
			state.running = false
			delete(manager.schedules, key)
			manager.lifecycleMu.Unlock()
			return
		}

		if directContinuations < directContinuationLimit {
			directContinuations++
			continuing = true
			manager.lifecycleMu.Unlock()
			continue
		}

		select {
		case next := <-manager.queue:
			// lifecycleMu excludes fresh admissions, so receiving next reserves
			// one queue slot for the accepted continuation without blocking.
			state.running = false
			state.queued = true
			manager.queue <- scope
			manager.lifecycleMu.Unlock()
			scope = next
			continuing = false
			directContinuations = 0
		default:
			continuing = true
			directContinuations = 0
			manager.lifecycleMu.Unlock()
		}
	}
}

type scheduleState struct {
	scope          memory.Scope
	queued         bool
	running        bool
	requestedEpoch uint64
}

// New validates immutable dependencies without starting workers or provider I/O.
func New(dependencies Dependencies) (*Manager, error) {
	if dependencies.Canonical == nil || dependencies.Records == nil || dependencies.Manifests == nil ||
		dependencies.Physical == nil ||
		dependencies.Provider == nil || dependencies.Store == nil {
		return nil, fmt.Errorf("semantic projection dependencies are required")
	}
	// A legacy nil *Store remains nil after conversion to the consumer port.
	if value := reflect.ValueOf(dependencies.Store); value.Kind() == reflect.Pointer && value.IsNil() {
		return nil, fmt.Errorf("semantic projection dependencies are required")
	}

	spaceID, err := semanticcore.SpaceFingerprint(dependencies.ModelSpace)
	if err != nil {
		return nil, fmt.Errorf("semantic projection model space: %w", err)
	}

	shutdown, stopAll := context.WithCancel(context.Background())

	return &Manager{
		canonical: dependencies.Canonical, records: dependencies.Records,
		manifests: dependencies.Manifests, physical: dependencies.Physical, provider: dependencies.Provider,
		store: dependencies.Store, space: dependencies.ModelSpace, spaceID: spaceID,
		buildSlots:   make(chan struct{}, semanticcore.SemanticWorkerLimit),
		buildTimeout: semanticcore.SemanticBuildTimeout,
		now:          time.Now,
		flights:      make(map[string]*flight), statuses: make(map[string]ScopeStatus),
		queue:     make(chan memory.Scope, semanticcore.SemanticQueueLimit),
		schedules: make(map[string]*scheduleState),
		shutdown:  shutdown, stopAll: stopAll,
	}, nil
}

// Repair synchronously rebuilds one exact scope through the same bounded,
// per-scope single-flight path used by background workers.
//
//nolint:wsl_v5 // Lifecycle admission is checked immediately before single-flight execution.
func (manager *Manager) Repair(ctx context.Context, scope memory.Scope) error {
	if manager == nil || ctx == nil {
		return fmt.Errorf("semantic projection repair context is required")
	}

	if err := scope.Validate(); err != nil {
		return err
	}

	manager.lifecycleMu.Lock()
	closed := manager.closed
	manager.lifecycleMu.Unlock()
	if closed {
		return ErrClosed
	}

	result := manager.runSingleFlight(ctx, scope, false)

	return result.err
}

//nolint:cyclop,funcorder,funlen,nlreturn,wsl_v5 // Single-flight ownership and cancellation branches remain explicit.
func (manager *Manager) runSingleFlight(ctx context.Context, scope memory.Scope, scheduled bool) flightResult {
	key := scopeKey(scope)

	manager.flightMu.Lock()
	if current := manager.flights[key]; current != nil {
		manager.flightMu.Unlock()
		if manager.joinedFlight != nil {
			manager.joinedFlight()
		}

		select {
		case <-current.done:
			return flightResult{
				completedEpoch: current.snapshotEpoch,
				err:            current.err,
				retryScheduled: scheduled && !current.ownerScheduled && current.callerCanceled,
			}
		case <-ctx.Done():
			return flightResult{err: ctx.Err()}
		}
	}

	manager.lifecycleMu.Lock()
	if manager.closed {
		manager.lifecycleMu.Unlock()
		manager.flightMu.Unlock()

		return flightResult{err: ErrClosed}
	}
	manager.builds.Add(1)
	manager.lifecycleMu.Unlock()

	current := &flight{
		done: make(chan struct{}), snapshotEpoch: manager.currentRequestEpoch(), ownerScheduled: scheduled,
	}
	manager.flights[key] = current
	manager.flightMu.Unlock()

	defer func() {
		current.callerCanceled = ctx.Err() != nil &&
			(errors.Is(current.err, context.Canceled) || errors.Is(current.err, context.DeadlineExceeded))
		manager.flightMu.Lock()
		delete(manager.flights, key)
		close(current.done)
		manager.flightMu.Unlock()
		manager.builds.Done()
	}()

	executionCtx, cancelExecution := context.WithCancel(ctx)
	stopShutdown := context.AfterFunc(manager.shutdown, cancelExecution)
	defer func() {
		stopShutdown()
		cancelExecution()
	}()

	buildCtx, cancelTimeout := context.WithTimeout(executionCtx, manager.buildTimeout)
	defer cancelTimeout()

	select {
	case manager.buildSlots <- struct{}{}:
		defer func() { <-manager.buildSlots }()
	case <-buildCtx.Done():
		current.err = buildCtx.Err()
		manager.recordBuildFailure(scope, buildTarget{}, current.err)
		return flightResult{completedEpoch: current.snapshotEpoch, err: current.err}
	}

	current.err = manager.build(buildCtx, scope, current)
	return flightResult{completedEpoch: current.snapshotEpoch, err: current.err}
}

//nolint:funcorder // Request epochs stay beside the single-flight mechanics they order.
func (manager *Manager) nextRequestEpoch() uint64 {
	manager.requestMu.Lock()
	defer manager.requestMu.Unlock()

	manager.requestEpoch++

	return manager.requestEpoch
}

//nolint:funcorder // Request epochs stay beside the single-flight mechanics they order.
func (manager *Manager) currentRequestEpoch() uint64 {
	manager.requestMu.Lock()
	defer manager.requestMu.Unlock()

	return manager.requestEpoch
}

func scopeKey(scope memory.Scope) string {
	return string(scope.Kind) + "\x00" + scope.Key
}

// Status reports one scope without enumerating or exposing source/provider text.
//
//nolint:cyclop,wsl_v5 // Lifecycle and compatibility gates intentionally fail closed in one bounded read.
func (manager *Manager) Status(ctx context.Context, scope memory.Scope) (ScopeStatus, error) {
	if manager == nil || ctx == nil {
		return ScopeStatus{}, fmt.Errorf("semantic projection status context is required")
	}

	if err := scope.Validate(); err != nil {
		return ScopeStatus{}, err
	}

	manager.lifecycleMu.Lock()
	closed := manager.closed
	manager.lifecycleMu.Unlock()
	if closed {
		return ScopeStatus{State: StateStopped, ModelSpace: manager.spaceID, UpdatedAt: manager.now().UTC()}, nil
	}

	manager.statusMu.RLock()
	status, statusFound := manager.statuses[scopeKey(scope)]
	manager.statusMu.RUnlock()
	if statusFound && status.State == StateRepairing {
		return status, nil
	}

	active, activeFound, err := manager.store.ActiveState(ctx, scope)
	if err != nil {
		return ScopeStatus{}, err
	}
	if !activeFound {
		if statusFound && status.State == StateDegraded {
			return status, nil
		}

		return ScopeStatus{
			State: StateDegraded, SafeReason: semanticcore.ReasonUnavailable,
			ModelSpace: manager.spaceID, UpdatedAt: time.Now().UTC(),
		}, nil
	}

	if active.Status != semanticcore.StatusReady || active.ModelSpace != manager.spaceID ||
		active.Dimensions != manager.space.Dimensions || manager.verifyView(ctx, active.View()) != nil ||
		!manager.observedMetadataReady(ctx, active.View()) {
		if statusFound && status.State == StateDegraded {
			return status, nil
		}

		return ScopeStatus{
			State: StateDegraded, SafeReason: semanticcore.ReasonUnavailable,
			LexicalGenerationID: active.LexicalGenerationID, LexicalWatermark: active.LexicalWatermark,
			ModelSpace: manager.spaceID, UpdatedAt: active.ReadyAt,
		}, nil
	}

	return ScopeStatus{
		State:               StateReady,
		LexicalGenerationID: active.LexicalGenerationID, LexicalWatermark: active.LexicalWatermark,
		ModelSpace: active.ModelSpace, UpdatedAt: active.ReadyAt,
	}, nil
}

//nolint:funcorder // Kept beside Status while public lifecycle methods remain grouped.
func (manager *Manager) setStatus(scope memory.Scope, status ScopeStatus) {
	manager.statusMu.Lock()

	previous := manager.statuses[scopeKey(scope)]
	if previous.State == StateRepairing {
		manager.repairingCount--
	}

	if previous.State == StateDegraded {
		manager.degradedCount--
	}

	if status.State == StateRepairing {
		manager.repairingCount++
	}

	if status.State == StateDegraded {
		manager.degradedCount++
	}

	manager.statuses[scopeKey(scope)] = status
	manager.statusMu.Unlock()
}

// Close cancels background work and waits until workers have observed it.
//
//nolint:wsl_v5 // Idempotence, cancellation, and bounded draining remain explicit.
func (manager *Manager) Close(ctx context.Context) error {
	if manager == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	done := manager.stop()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (manager *Manager) stop() <-chan struct{} {
	manager.lifecycleMu.Lock()
	if manager.closed {
		done := manager.done
		manager.lifecycleMu.Unlock()

		return done
	}

	manager.closed = true
	manager.started = false
	cancel := manager.cancel
	stopAll := manager.stopAll
	manager.done = make(chan struct{})

	done := manager.done
	go manager.finishStop(done)
	manager.lifecycleMu.Unlock()

	if cancel != nil {
		cancel()
	}

	if stopAll != nil {
		stopAll()
	}

	return done
}

func (manager *Manager) finishStop(done chan struct{}) {
	manager.workers.Wait()
	manager.builds.Wait()

	manager.lifecycleMu.Lock()
	clear(manager.schedules)
	manager.lifecycleMu.Unlock()
	manager.markStatusesStopped()
	close(done)
}

func (manager *Manager) markStatusesStopped() {
	manager.statusMu.Lock()
	defer manager.statusMu.Unlock()

	for key, status := range manager.statuses {
		status.State = StateStopped
		status.SafeReason = ""
		status.UpdatedAt = manager.now().UTC()
		manager.statuses[key] = status
	}
}
