package app

import (
	"context"
	"sync"
	"time"

	"github.com/baldaworks/boyan"
)

const defaultProjectionRepairRetryDelay = 100 * time.Millisecond

// SequencedIngestConfig controls the process-boundary ordered-ingest services.
type SequencedIngestConfig struct {
	Lanes                ScopeLaneConfig
	ProjectionRetryDelay time.Duration
}

// SequencedIngestRuntime provides producer/scope ordered turn and boundary
// ingestion with durable replay and projection-lag separation.
type SequencedIngestRuntime struct {
	sequences boyan.IngestSequenceStore
	canonical boyan.CanonicalStore
	lanes     *scopeLaneExecutor

	turn struct {
		processor  sequencedTurnCanonicalProcessor
		derivation boyan.DerivationRef
		projection ProjectionSyncer
		repair     *projectionRepairer
	}

	boundary struct {
		processor  BoundaryExecutor
		projection ProjectionSyncer
		repair     *projectionRepairer
	}
}

type sequencedTurnCanonicalProcessor interface {
	ProcessTurn(ctx context.Context, turn boyan.Turn, derivation boyan.DerivationRef) (boyan.CanonicalMutationOutcome, error)
}

// NewSequencedIngestRuntime constructs the process-boundary app service over
// existing portable processors and a durable ingest sequence store.
func NewSequencedIngestRuntime(sequences boyan.IngestSequenceStore, canonical boyan.CanonicalStore, turn *TurnProcessor, boundary *BoundaryService, config SequencedIngestConfig) (*SequencedIngestRuntime, error) {
	var turnProcessor sequencedTurnCanonicalProcessor
	var derivation boyan.DerivationRef
	var turnProjection ProjectionSyncer
	if turn != nil {
		turnProcessor = turn.processor
		derivation = turn.derivation
		turnProjection = turn.projection
	}
	var boundaryProcessor BoundaryExecutor
	var boundaryProjection ProjectionSyncer
	if boundary != nil {
		boundaryProcessor = boundary.processor
		boundaryProjection = boundary.projection
	}
	return newSequencedIngestRuntimeWithParts(sequences, canonical, turnProcessor, derivation, turnProjection, boundaryProcessor, boundaryProjection, config)
}

func newSequencedIngestRuntimeWithParts(sequences boyan.IngestSequenceStore, canonical boyan.CanonicalStore, turnProcessor sequencedTurnCanonicalProcessor, derivation boyan.DerivationRef, turnProjection ProjectionSyncer, boundaryProcessor BoundaryExecutor, boundaryProjection ProjectionSyncer, config SequencedIngestConfig) (*SequencedIngestRuntime, error) {
	if sequences == nil || canonical == nil {
		return nil, boyan.PermanentError(boyan.CodeStoreFailure, "sequenced ingest dependencies are required", nil)
	}
	if turnProcessor == nil && boundaryProcessor == nil {
		return nil, boyan.PermanentError(boyan.CodeDisabled, "sequenced ingest requires at least one capability", nil)
	}
	lanes, err := newScopeLaneExecutor(config.Lanes)
	if err != nil {
		return nil, err
	}
	retryDelay := config.ProjectionRetryDelay
	if retryDelay <= 0 {
		retryDelay = defaultProjectionRepairRetryDelay
	}
	runtime := &SequencedIngestRuntime{sequences: sequences, canonical: canonical, lanes: lanes}
	if turnProcessor != nil {
		if derivation == (boyan.DerivationRef{}) {
			derivation = boyan.LegacyDerivationRef()
		}
		if err := derivation.Validate(); err != nil {
			return nil, err
		}
		runtime.turn.processor = turnProcessor
		runtime.turn.derivation = derivation
		runtime.turn.projection = turnProjection
		if turnProjection != nil {
			runtime.turn.repair = newProjectionRepairer(turnProjection, retryDelay)
		}
	}
	if boundaryProcessor != nil {
		runtime.boundary.processor = boundaryProcessor
		runtime.boundary.projection = boundaryProjection
		if boundaryProjection != nil {
			runtime.boundary.repair = newProjectionRepairer(boundaryProjection, retryDelay)
		}
	}
	return runtime, nil
}

func (r *SequencedIngestRuntime) Start(ctx context.Context) error {
	if r == nil {
		return boyan.PermanentError(boyan.CodeDisabled, "sequenced ingest runtime is unavailable", nil)
	}
	if err := r.lanes.Start(ctx); err != nil {
		return err
	}
	if r.turn.repair != nil {
		if err := r.turn.repair.Start(ctx); err != nil {
			_ = r.lanes.Close(context.Background())
			return err
		}
	}
	if r.boundary.repair != nil && r.boundary.repair != r.turn.repair {
		if err := r.boundary.repair.Start(ctx); err != nil {
			if r.turn.repair != nil {
				_ = r.turn.repair.Close(context.Background())
			}
			_ = r.lanes.Close(context.Background())
			return err
		}
	}
	return nil
}

func (r *SequencedIngestRuntime) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	var firstErr error
	if r.boundary.repair != nil && r.boundary.repair != r.turn.repair {
		if err := r.boundary.repair.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if r.turn.repair != nil {
		if err := r.turn.repair.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := r.lanes.Close(ctx); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func (r *SequencedIngestRuntime) IngestSequencedTurn(ctx context.Context, request boyan.SequencedTurn) (boyan.IngestOutcome, error) {
	if r == nil || r.turn.processor == nil {
		return boyan.IngestOutcome{}, boyan.PermanentError(boyan.CodeDisabled, "sequenced turn ingestion is unavailable", nil)
	}
	if err := request.Validate(); err != nil {
		return boyan.IngestOutcome{}, err
	}
	var outcome boyan.IngestOutcome
	err := r.lanes.Execute(ctx, request.Turn.Scope, func(runCtx context.Context) error {
		var ingestErr error
		outcome, ingestErr = r.ingestTurn(runCtx, request)
		return ingestErr
	})
	return outcome, err
}

func (r *SequencedIngestRuntime) ApplySequencedBoundary(ctx context.Context, request boyan.SequencedBoundary) (boyan.IngestOutcome, error) {
	if r == nil || r.boundary.processor == nil {
		return boyan.IngestOutcome{}, boyan.PermanentError(boyan.CodeDisabled, "sequenced boundary ingestion is unavailable", nil)
	}
	if err := request.Validate(); err != nil {
		return boyan.IngestOutcome{}, err
	}
	var outcome boyan.IngestOutcome
	err := r.lanes.Execute(ctx, request.Boundary.Scope, func(runCtx context.Context) error {
		var ingestErr error
		outcome, ingestErr = r.ingestBoundary(runCtx, request)
		return ingestErr
	})
	return outcome, err
}

func (r *SequencedIngestRuntime) ingestTurn(ctx context.Context, request boyan.SequencedTurn) (boyan.IngestOutcome, error) {
	if replay, ok, err := r.loadReplay(ctx, request.ProducerID, request.Turn.Scope, request.ScopeSequence, request.Fingerprint); ok || err != nil {
		return replay, err
	}
	canonicalOutcome, err := r.turn.processor.ProcessTurn(ctx, request.Turn, r.turn.derivation)
	if err != nil {
		return boyan.IngestOutcome{}, err
	}
	operationID, err := boyan.CanonicalSemanticOperationID(request.Turn.ExportID, r.turn.derivation)
	if err != nil {
		return boyan.IngestOutcome{}, err
	}
	outcome := boyan.IngestOutcome{
		SchemaVersion:  boyan.IngestOutcomeSchemaVersionV1,
		Kind:           boyan.IngestKindTurn,
		Disposition:    boyan.IngestDispositionAccepted,
		Projection:     boyan.ProjectionStatusCurrent,
		ProducerID:     request.ProducerID,
		Scope:          request.Turn.Scope,
		ScopeSequence:  request.ScopeSequence,
		Fingerprint:    request.Fingerprint,
		ExportID:       request.Turn.ExportID,
		OperationIDs:   []string{operationID},
		ScopeVersion:   canonicalOutcome.ScopeVersion,
		ScopeChangeSeq: canonicalOutcome.ChangeSeq,
	}
	if err := outcome.Validate(); err != nil {
		return boyan.IngestOutcome{}, err
	}
	outcome = r.markProjectionStatus(ctx, request.Turn.Scope, outcome, r.turn.projection, r.turn.repair)
	if err := r.sequences.SaveIngestSequence(ctx, boyan.IngestSequenceRecord{
		SchemaVersion: boyan.IngestSequenceSchemaVersionV1,
		ProducerID:    request.ProducerID,
		Scope:         request.Turn.Scope,
		Sequence:      request.ScopeSequence,
		Fingerprint:   request.Fingerprint,
		Outcome:       outcome,
	}); err != nil {
		return boyan.IngestOutcome{}, err
	}
	return outcome, nil
}

func (r *SequencedIngestRuntime) ingestBoundary(ctx context.Context, request boyan.SequencedBoundary) (boyan.IngestOutcome, error) {
	if replay, ok, err := r.loadReplay(ctx, request.ProducerID, request.Boundary.Scope, request.ScopeSequence, request.Fingerprint); ok || err != nil {
		return replay, err
	}
	boundaryOutcome, err := r.boundary.processor.ProcessBoundary(ctx, request.Boundary)
	if err != nil {
		return boyan.IngestOutcome{}, err
	}
	state, err := r.canonical.LoadScopeState(ctx, request.Boundary.Scope)
	if err != nil {
		return boyan.IngestOutcome{}, err
	}
	operationIDs := make([]string, 0, 2)
	if boundaryOutcome.Scenarios.OperationID != "" {
		operationIDs = append(operationIDs, boundaryOutcome.Scenarios.OperationID)
	}
	if boundaryOutcome.Profile.OperationID != "" {
		operationIDs = append(operationIDs, boundaryOutcome.Profile.OperationID)
	}
	outcome := boyan.IngestOutcome{
		SchemaVersion:  boyan.IngestOutcomeSchemaVersionV1,
		Kind:           boyan.IngestKindBoundary,
		Disposition:    boyan.IngestDispositionAccepted,
		Projection:     boyan.ProjectionStatusCurrent,
		ProducerID:     request.ProducerID,
		Scope:          request.Boundary.Scope,
		ScopeSequence:  request.ScopeSequence,
		Fingerprint:    request.Fingerprint,
		ExportID:       request.Boundary.ExportID,
		OperationIDs:   operationIDs,
		ScopeVersion:   state.Version,
		ScopeChangeSeq: state.ChangeSeq,
	}
	if err := outcome.Validate(); err != nil {
		return boyan.IngestOutcome{}, err
	}
	outcome = r.markProjectionStatus(ctx, request.Boundary.Scope, outcome, r.boundary.projection, r.boundary.repair)
	if err := r.sequences.SaveIngestSequence(ctx, boyan.IngestSequenceRecord{
		SchemaVersion: boyan.IngestSequenceSchemaVersionV1,
		ProducerID:    request.ProducerID,
		Scope:         request.Boundary.Scope,
		Sequence:      request.ScopeSequence,
		Fingerprint:   request.Fingerprint,
		Outcome:       outcome,
	}); err != nil {
		return boyan.IngestOutcome{}, err
	}
	return outcome, nil
}

func (r *SequencedIngestRuntime) loadReplay(ctx context.Context, producerID string, scope boyan.Scope, sequence uint64, fingerprint string) (boyan.IngestOutcome, bool, error) {
	cursor, found, err := r.sequences.LoadLatestIngestSequence(ctx, producerID, scope)
	if err != nil {
		return boyan.IngestOutcome{}, false, err
	}
	next := uint64(1)
	if found {
		next = cursor.Latest + 1
	}
	switch {
	case sequence > next:
		return boyan.IngestOutcome{}, true, boyan.RetryableError(boyan.CodeConflict, "ingest sequence gap", nil)
	case sequence == next:
		return boyan.IngestOutcome{}, false, nil
	default:
		record, recordFound, err := r.sequences.LoadIngestSequence(ctx, producerID, scope, sequence)
		if err != nil {
			return boyan.IngestOutcome{}, true, err
		}
		if !recordFound {
			return boyan.IngestOutcome{}, true, boyan.PermanentError(boyan.CodeConflict, "ingest sequence regressed", nil)
		}
		if record.Fingerprint != fingerprint {
			return boyan.IngestOutcome{}, true, boyan.PermanentError(boyan.CodeConflict, "ingest sequence fingerprint conflicted", nil)
		}
		outcome := record.Outcome
		outcome.Disposition = boyan.IngestDispositionDuplicate
		return outcome, true, nil
	}
}

func (r *SequencedIngestRuntime) markProjectionStatus(ctx context.Context, scope boyan.Scope, outcome boyan.IngestOutcome, projection ProjectionSyncer, repair *projectionRepairer) boyan.IngestOutcome {
	if projection == nil {
		return outcome
	}
	if err := projection.Sync(ctx, scope); err != nil {
		outcome.Projection = boyan.ProjectionStatusLagging
		if repair != nil {
			_ = repair.Mark(scope)
		}
	}
	return outcome
}

type projectionRepairer struct {
	syncer     ProjectionSyncer
	retryDelay time.Duration

	mu      sync.Mutex
	started bool
	closed  bool
	pending map[string]boyan.Scope
	signal  chan struct{}
	cancel  context.CancelFunc
	done    chan struct{}
}

func newProjectionRepairer(syncer ProjectionSyncer, retryDelay time.Duration) *projectionRepairer {
	if retryDelay <= 0 {
		retryDelay = defaultProjectionRepairRetryDelay
	}
	return &projectionRepairer{
		syncer:     syncer,
		retryDelay: retryDelay,
		pending:    make(map[string]boyan.Scope),
		signal:     make(chan struct{}),
	}
}

func (r *projectionRepairer) Start(parent context.Context) error {
	if r == nil {
		return nil
	}
	if parent == nil {
		parent = context.Background()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return boyan.PermanentError(boyan.CodeShuttingDown, "projection repairer is closed", nil)
	}
	if r.started {
		return nil
	}
	ctx, cancel := context.WithCancel(parent)
	r.cancel = cancel
	r.done = make(chan struct{})
	r.started = true
	go func(done chan struct{}) {
		defer close(done)
		r.run(ctx)
	}(r.done)
	return nil
}

func (r *projectionRepairer) Mark(scope boyan.Scope) error {
	if r == nil {
		return nil
	}
	if err := scope.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.started {
		return boyan.PermanentError(boyan.CodeDisabled, "projection repairer is not started", nil)
	}
	if r.closed {
		return boyan.PermanentError(boyan.CodeShuttingDown, "projection repairer is closing", nil)
	}
	r.pending[string(scope.Kind)+"\x00"+scope.Key] = scope
	r.signalLocked()
	return nil
}

func (r *projectionRepairer) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	cancel := r.cancel
	done := r.done
	r.signalLocked()
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return boyan.RetryableError(boyan.CodeTimeout, "projection repairer close timed out", ctx.Err())
	}
}

func (r *projectionRepairer) run(ctx context.Context) {
	for {
		scope, ok := r.next(ctx)
		if !ok {
			return
		}
		if err := r.syncer.Sync(ctx, scope); err != nil {
			if ctx.Err() != nil {
				return
			}
			timer := time.NewTimer(r.retryDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				r.requeue(scope)
				return
			case <-timer.C:
			}
			r.requeue(scope)
		}
	}
}

func (r *projectionRepairer) next(ctx context.Context) (boyan.Scope, bool) {
	for {
		r.mu.Lock()
		for key, scope := range r.pending {
			delete(r.pending, key)
			r.mu.Unlock()
			return scope, true
		}
		signal := r.signal
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return boyan.Scope{}, false
		case <-signal:
		}
	}
}

func (r *projectionRepairer) requeue(scope boyan.Scope) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.pending[string(scope.Kind)+"\x00"+scope.Key] = scope
	r.signalLocked()
}

func (r *projectionRepairer) signalLocked() {
	close(r.signal)
	r.signal = make(chan struct{})
}

var _ SequencedTurnIngestor = (*SequencedIngestRuntime)(nil)
var _ SequencedBoundaryIngestor = (*SequencedIngestRuntime)(nil)
var _ Lifecycle = (*SequencedIngestRuntime)(nil)
