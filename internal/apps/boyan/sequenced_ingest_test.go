package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/baldaworks/boyan"
)

func TestSequencedIngestRuntimeAcceptsAndReplaysTurn(t *testing.T) {
	t.Parallel()

	sequences := newMemoryIngestSequenceStore()
	runtime, err := newSequencedIngestRuntimeWithParts(sequences, fakeCanonicalStore{}, fakeTurnProcessor{outcome: boyan.CanonicalMutationOutcome{ScopeVersion: 1, ChangeSeq: 1}}, boyan.LegacyDerivationRef(), nil, nil, nil, SequencedIngestConfig{})
	if err != nil {
		t.Fatalf("NewSequencedIngestRuntime() error = %v", err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	request := testSequencedTurn(t, 1, "fingerprint-1")
	first, err := runtime.IngestSequencedTurn(context.Background(), request)
	if err != nil {
		t.Fatalf("IngestSequencedTurn(first) error = %v", err)
	}
	if first.Disposition != boyan.IngestDispositionAccepted || first.Projection != boyan.ProjectionStatusCurrent || first.ScopeVersion != 1 || first.ScopeChangeSeq != 1 {
		t.Fatalf("first outcome = %+v, want accepted/current with canonical watermark", first)
	}

	second, err := runtime.IngestSequencedTurn(context.Background(), request)
	if err != nil {
		t.Fatalf("IngestSequencedTurn(replay) error = %v", err)
	}
	if second.Disposition != boyan.IngestDispositionDuplicate || second.ScopeSequence != 1 || second.Fingerprint != request.Fingerprint {
		t.Fatalf("duplicate outcome = %+v, want duplicate replay for same sequence/fingerprint", second)
	}
}

func TestSequencedIngestRuntimeRejectsGapAndFingerprintConflict(t *testing.T) {
	t.Parallel()

	sequences := newMemoryIngestSequenceStore()
	runtime, err := newSequencedIngestRuntimeWithParts(sequences, fakeCanonicalStore{}, fakeTurnProcessor{outcome: boyan.CanonicalMutationOutcome{ScopeVersion: 1, ChangeSeq: 1}}, boyan.LegacyDerivationRef(), nil, nil, nil, SequencedIngestConfig{})
	if err != nil {
		t.Fatalf("NewSequencedIngestRuntime() error = %v", err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	gapRequest := testSequencedTurn(t, 2, "fingerprint-gap")
	if _, err := runtime.IngestSequencedTurn(context.Background(), gapRequest); !hasCodeClass(err, boyan.CodeConflict, boyan.ErrorClassRetryable) {
		t.Fatalf("gap error = %v, want retryable conflict", err)
	}

	first := testSequencedTurn(t, 1, "fingerprint-1")
	if _, err := runtime.IngestSequencedTurn(context.Background(), first); err != nil {
		t.Fatalf("IngestSequencedTurn(first) error = %v", err)
	}
	conflict := testSequencedTurn(t, 1, "fingerprint-conflict")
	if _, err := runtime.IngestSequencedTurn(context.Background(), conflict); !hasCodeClass(err, boyan.CodeConflict, boyan.ErrorClassPermanent) {
		t.Fatalf("conflict error = %v, want permanent conflict", err)
	}
}

func TestSequencedIngestRuntimeSeparatesProjectionLagAndSchedulesRepair(t *testing.T) {
	t.Parallel()

	sequences := newMemoryIngestSequenceStore()
	projection := &flakyProjectionSyncer{failuresRemaining: 1, synced: make(chan struct{}, 2)}
	runtime, err := newSequencedIngestRuntimeWithParts(sequences, fakeCanonicalStore{}, fakeTurnProcessor{outcome: boyan.CanonicalMutationOutcome{ScopeVersion: 1, ChangeSeq: 1}}, boyan.LegacyDerivationRef(), projection, nil, nil, SequencedIngestConfig{ProjectionRetryDelay: 10 * time.Millisecond})
	if err != nil {
		t.Fatalf("NewSequencedIngestRuntime() error = %v", err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	outcome, err := runtime.IngestSequencedTurn(context.Background(), testSequencedTurn(t, 1, "fingerprint-lag"))
	if err != nil {
		t.Fatalf("IngestSequencedTurn() error = %v", err)
	}
	if outcome.Disposition != boyan.IngestDispositionAccepted || outcome.Projection != boyan.ProjectionStatusLagging {
		t.Fatalf("outcome = %+v, want accepted/lagging after projection failure", outcome)
	}
	waitForProjectionSync(t, projection, 2)
}

func TestSequencedIngestRuntimeCloseRejectsNewWork(t *testing.T) {
	t.Parallel()

	sequences := newMemoryIngestSequenceStore()
	runtime, err := newSequencedIngestRuntimeWithParts(sequences, fakeCanonicalStore{}, fakeTurnProcessor{outcome: boyan.CanonicalMutationOutcome{ScopeVersion: 1, ChangeSeq: 1}}, boyan.LegacyDerivationRef(), nil, nil, nil, SequencedIngestConfig{})
	if err != nil {
		t.Fatalf("NewSequencedIngestRuntime() error = %v", err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := runtime.IngestSequencedTurn(context.Background(), testSequencedTurn(t, 1, "fingerprint-close")); !hasCodeClass(err, boyan.CodeShuttingDown, boyan.ErrorClassPermanent) {
		t.Fatalf("IngestSequencedTurn(after close) error = %v, want shutting_down", err)
	}
}

func TestSequencedIngestRuntimeSameScopeRequestsStayFIFO(t *testing.T) {
	t.Parallel()

	sequences := newMemoryIngestSequenceStore()
	firstStarted := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	order := make([]string, 0, 2)
	runtime, err := newSequencedIngestRuntimeWithParts(sequences, fakeCanonicalStore{}, fakeTurnProcessor{
		process: func(_ context.Context, turn boyan.Turn, _ boyan.DerivationRef) (boyan.CanonicalMutationOutcome, error) {
			mu.Lock()
			order = append(order, turn.SourceTurnID)
			mu.Unlock()
			if turn.SourceTurnID == "turn-1" {
				close(firstStarted)
				<-release
			}
			return boyan.CanonicalMutationOutcome{ScopeVersion: uint64(len(order)), ChangeSeq: uint64(len(order))}, nil
		},
	}, boyan.LegacyDerivationRef(), nil, nil, nil, SequencedIngestConfig{})
	if err != nil {
		t.Fatalf("newSequencedIngestRuntimeWithParts() error = %v", err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	errs := make(chan error, 2)
	go func() {
		_, err := runtime.IngestSequencedTurn(context.Background(), testSequencedTurnWithID(t, 1, "fifo-fingerprint-1", "turn-1"))
		errs <- err
	}()
	<-firstStarted
	go func() {
		_, err := runtime.IngestSequencedTurn(context.Background(), testSequencedTurnWithID(t, 2, "fifo-fingerprint-2", "turn-2"))
		errs <- err
	}()
	time.Sleep(10 * time.Millisecond)
	close(release)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("IngestSequencedTurn() error = %v", err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "turn-1" || order[1] != "turn-2" {
		t.Fatalf("process order = %v, want [turn-1 turn-2]", order)
	}
}

func TestSequencedIngestRuntimeAcceptsBoundaryAndPersistsReplay(t *testing.T) {
	t.Parallel()

	scope := boyan.Scope{Key: "telegram:sequenced-boundary", Kind: boyan.ScopeKindPersonal}
	sequences := newMemoryIngestSequenceStore()
	runtime, err := newSequencedIngestRuntimeWithParts(
		sequences,
		fakeCanonicalStore{state: boyan.ScopeState{SchemaVersion: boyan.CanonicalSchemaVersionV1, Scope: scope, Version: 2, ChangeSeq: 3}},
		nil,
		boyan.DerivationRef{},
		nil,
		fakeBoundaryProcessor{outcome: boyan.BoundaryOutcome{
			SchemaVersion: boyan.DerivedSchemaVersionV1,
			Scope:         scope,
			Scenarios:     boyan.OperationOutcome{SchemaVersion: boyan.DerivedSchemaVersionV1, OperationID: "scenario-op", Stage: boyan.OperationStageScenarios, Scope: scope, ScopeVersion: 1},
			Profile:       boyan.OperationOutcome{SchemaVersion: boyan.DerivedSchemaVersionV1, OperationID: "profile-op", Stage: boyan.OperationStageProfile, Scope: scope, ScopeVersion: 2},
		}},
		nil,
		SequencedIngestConfig{},
	)
	if err != nil {
		t.Fatalf("newSequencedIngestRuntimeWithParts() error = %v", err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	request := testSequencedBoundary(t, 1, "boundary-fingerprint")
	first, err := runtime.ApplySequencedBoundary(context.Background(), request)
	if err != nil {
		t.Fatalf("ApplySequencedBoundary(first) error = %v", err)
	}
	if first.Disposition != boyan.IngestDispositionAccepted || first.ScopeVersion != 2 || first.ScopeChangeSeq != 3 || len(first.OperationIDs) != 2 {
		t.Fatalf("first boundary outcome = %+v, want accepted replayable canonical state", first)
	}

	second, err := runtime.ApplySequencedBoundary(context.Background(), request)
	if err != nil {
		t.Fatalf("ApplySequencedBoundary(replay) error = %v", err)
	}
	if second.Disposition != boyan.IngestDispositionDuplicate {
		t.Fatalf("duplicate boundary outcome = %+v, want duplicate replay", second)
	}
}

type fakeTurnProcessor struct {
	outcome boyan.CanonicalMutationOutcome
	err     error
	process func(context.Context, boyan.Turn, boyan.DerivationRef) (boyan.CanonicalMutationOutcome, error)
}

func (f fakeTurnProcessor) ProcessTurn(ctx context.Context, turn boyan.Turn, derivation boyan.DerivationRef) (boyan.CanonicalMutationOutcome, error) {
	if f.process != nil {
		return f.process(ctx, turn, derivation)
	}
	return f.outcome, f.err
}

type fakeBoundaryProcessor struct {
	outcome boyan.BoundaryOutcome
	err     error
}

func (f fakeBoundaryProcessor) ProcessBoundary(context.Context, boyan.Boundary) (boyan.BoundaryOutcome, error) {
	return f.outcome, f.err
}

type memoryIngestSequenceStore struct {
	mu      sync.Mutex
	cursors map[string]boyan.IngestSequenceCursor
	records map[string]boyan.IngestSequenceRecord
}

func newMemoryIngestSequenceStore() *memoryIngestSequenceStore {
	return &memoryIngestSequenceStore{
		cursors: make(map[string]boyan.IngestSequenceCursor),
		records: make(map[string]boyan.IngestSequenceRecord),
	}
}

func (s *memoryIngestSequenceStore) LoadLatestIngestSequence(_ context.Context, producerID string, scope boyan.Scope) (boyan.IngestSequenceCursor, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cursor, ok := s.cursors[sequenceCursorKey(producerID, scope)]
	return cursor, ok, nil
}

func (s *memoryIngestSequenceStore) LoadIngestSequence(_ context.Context, producerID string, scope boyan.Scope, sequence uint64) (boyan.IngestSequenceRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[sequenceRecordKey(producerID, scope, sequence)]
	return record, ok, nil
}

func (s *memoryIngestSequenceStore) SaveIngestSequence(_ context.Context, record boyan.IngestSequenceRecord) error {
	if err := record.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	recordKey := sequenceRecordKey(record.ProducerID, record.Scope, record.Sequence)
	if stored, ok := s.records[recordKey]; ok {
		if stored.Fingerprint != record.Fingerprint {
			return boyan.PermanentError(boyan.CodeConflict, "ingest sequence identity was reused", nil)
		}
		return nil
	}
	cursorKey := sequenceCursorKey(record.ProducerID, record.Scope)
	cursor, ok := s.cursors[cursorKey]
	switch {
	case !ok && record.Sequence != 1:
		return boyan.PermanentError(boyan.CodeConflict, "ingest sequence is not next", nil)
	case ok && record.Sequence != cursor.Latest+1:
		return boyan.PermanentError(boyan.CodeConflict, "ingest sequence is not next", nil)
	}
	s.records[recordKey] = record
	s.cursors[cursorKey] = boyan.IngestSequenceCursor{
		SchemaVersion: boyan.IngestSequenceSchemaVersionV1,
		ProducerID:    record.ProducerID,
		Scope:         record.Scope,
		Latest:        record.Sequence,
	}
	return nil
}

type flakyProjectionSyncer struct {
	mu                sync.Mutex
	failuresRemaining int
	attempts          int
	synced            chan struct{}
}

func (s *flakyProjectionSyncer) Sync(context.Context, boyan.Scope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts++
	select {
	case s.synced <- struct{}{}:
	default:
	}
	if s.failuresRemaining > 0 {
		s.failuresRemaining--
		return errors.New("projection lag")
	}
	return nil
}

type fakeCanonicalStore struct {
	state boyan.ScopeState
	err   error
}

func (s fakeCanonicalStore) LoadScopeState(_ context.Context, scope boyan.Scope) (boyan.ScopeState, error) {
	if s.err != nil {
		return boyan.ScopeState{}, s.err
	}
	if s.state != (boyan.ScopeState{}) {
		return s.state, nil
	}
	return boyan.ScopeState{}, boyan.PermanentError(boyan.CodeDisabled, "unused in turn test", nil)
}
func (fakeCanonicalStore) ApplyCanonicalMutation(context.Context, boyan.CanonicalMutation) (boyan.CanonicalMutationOutcome, error) {
	return boyan.CanonicalMutationOutcome{}, boyan.PermanentError(boyan.CodeDisabled, "unused in turn test", nil)
}
func (fakeCanonicalStore) ScanScopeChanges(context.Context, boyan.Scope, uint64, uint32) ([]boyan.ScopeChange, error) {
	return nil, boyan.PermanentError(boyan.CodeDisabled, "unused in turn test", nil)
}
func (fakeCanonicalStore) LoadCanonicalRevisions(context.Context, boyan.CanonicalRevisionReadRequest) ([]boyan.MemoryRevision, error) {
	return nil, boyan.PermanentError(boyan.CodeDisabled, "unused in turn test", nil)
}
func (fakeCanonicalStore) ScanActiveHeads(context.Context, boyan.ActiveHeadScanRequest) ([]boyan.ItemHead, error) {
	return nil, boyan.PermanentError(boyan.CodeDisabled, "unused in turn test", nil)
}
func (fakeCanonicalStore) ScanActiveMemory(context.Context, boyan.ActiveMemoryScanRequest) ([]boyan.ActiveCanonicalMemory, error) {
	return nil, boyan.PermanentError(boyan.CodeDisabled, "unused in turn test", nil)
}
func (fakeCanonicalStore) ClaimDeliveryOutbox(context.Context, boyan.DeliveryClaimRequest) ([]boyan.ClaimedDelivery, error) {
	return nil, boyan.PermanentError(boyan.CodeDisabled, "unused in turn test", nil)
}
func (fakeCanonicalStore) SettleDeliveryOutbox(context.Context, boyan.DeliverySettlementRequest) error {
	return boyan.PermanentError(boyan.CodeDisabled, "unused in turn test", nil)
}

func sequenceCursorKey(producerID string, scope boyan.Scope) string {
	return producerID + "\x00" + string(scope.Kind) + "\x00" + scope.Key
}

func sequenceRecordKey(producerID string, scope boyan.Scope, sequence uint64) string {
	return sequenceCursorKey(producerID, scope) + "\x00" + time.Unix(int64(sequence), 0).UTC().Format(time.RFC3339Nano)
}

func hasCodeClass(err error, code boyan.ErrorCode, class boyan.ErrorClass) bool {
	gotCode, gotClass, ok := boyan.ClassifyError(err)
	return ok && gotCode == code && gotClass == class
}

func waitForProjectionSync(t *testing.T, syncer *flakyProjectionSyncer, want int) {
	t.Helper()
	deadline := time.After(500 * time.Millisecond)
	for {
		syncer.mu.Lock()
		got := syncer.attempts
		syncer.mu.Unlock()
		if got >= want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("projection sync attempts = %d, want >= %d", got, want)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func testSequencedTurn(t *testing.T, sequence uint64, fingerprint string) boyan.SequencedTurn {
	return testSequencedTurnWithID(t, sequence, fingerprint, "turn-1")
}

func testSequencedTurnWithID(t *testing.T, sequence uint64, fingerprint, turnID string) boyan.SequencedTurn {
	t.Helper()
	turn, err := boyan.NewTurn(
		boyan.Scope{Key: "telegram:sequenced-ingest", Kind: boyan.ScopeKindPersonal},
		boyan.SessionRef{SessionID: "session-1", AgentSessionID: "agent-1"},
		turnID,
		time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC),
		"Ada",
		"Ada lives in Bishkek",
	)
	if err != nil {
		t.Fatalf("NewTurn() error = %v", err)
	}
	return boyan.SequencedTurn{
		ProducerID:    "producer-1",
		ScopeSequence: sequence,
		Fingerprint:   fingerprint,
		Turn:          turn,
	}
}

func testSequencedBoundary(t *testing.T, sequence uint64, fingerprint string) boyan.SequencedBoundary {
	t.Helper()
	boundary, err := boyan.NewBoundary(
		boyan.Scope{Key: "telegram:sequenced-boundary", Kind: boyan.ScopeKindPersonal},
		boyan.SessionRef{SessionID: "session-boundary", AgentSessionID: "agent-boundary"},
		"transition-1",
		boyan.BoundaryReasonClose,
		time.Date(2026, time.August, 9, 12, 5, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("NewBoundary() error = %v", err)
	}
	return boyan.SequencedBoundary{
		ProducerID:    "producer-1",
		ScopeSequence: sequence,
		Fingerprint:   fingerprint,
		Boundary:      boundary,
	}
}
