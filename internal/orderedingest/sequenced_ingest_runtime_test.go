package orderedingest //nolint:testpackage // Exercises newSequencedIngestRuntimeWithParts within the package.

import (
	"context"
	"sync"
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func TestSequencedIngestRuntimeSeparatesProjectionLagAndSchedulesRepair(t *testing.T) {
	t.Parallel()

	sequences := newMemoryIngestSequenceStore()
	projection := &flakyProjectionSyncer{failuresRemaining: 1, synced: make(chan struct{}, 2)}

	runtime, err := newSequencedIngestRuntimeWithParts(
		sequences,
		fakeCanonicalStore{},
		fakeTurnProcessor{outcome: boyan.CanonicalMutationOutcome{ScopeVersion: 1, ChangeSeq: 1}},
		boyan.LegacyDerivationRef(),
		projection,
		nil,
		nil,
		SequencedIngestConfig{ProjectionRetryDelay: 10 * time.Millisecond},
	)
	requireNoError(t, err, "NewSequencedIngestRuntime() error = %v")

	requireNoError(t, runtime.Start(context.Background()), "Start() error = %v")

	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	outcome, err := runtime.IngestSequencedTurn(context.Background(), testSequencedTurn(t, 1, "fingerprint-lag"))
	requireNoError(t, err, "IngestSequencedTurn() error = %v")

	if outcome.Disposition != boyan.IngestDispositionAccepted ||
		outcome.Projection != boyan.ProjectionStatusLagging {
		t.Fatalf("outcome = %+v, want accepted/lagging after projection failure", outcome)
	}

	waitForProjectionSync(t, projection, 2)
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
	requireNoError(t, err, "newSequencedIngestRuntimeWithParts() error = %v")

	requireNoError(t, runtime.Start(context.Background()), "Start() error = %v")

	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	errs := make(chan error, 2)

	go submitFIFOTestTurn(t, runtime, 1, "fifo-fingerprint-1", "turn-1", errs)

	<-firstStarted

	go submitFIFOTestTurn(t, runtime, 2, "fifo-fingerprint-2", "turn-2", errs)

	time.Sleep(10 * time.Millisecond)
	close(release)

	for range 2 {
		requireNoError(t, <-errs, "IngestSequencedTurn() error = %v")
	}

	mu.Lock()
	defer mu.Unlock()

	if len(order) != 2 || order[0] != "turn-1" || order[1] != "turn-2" {
		t.Fatalf("process order = %v, want [turn-1 turn-2]", order)
	}
}

func submitFIFOTestTurn(
	t *testing.T,
	runtime *SequencedIngestRuntime,
	sequence uint64,
	fingerprint,
	turnID string,
	errs chan<- error,
) {
	t.Helper()

	_, err := runtime.IngestSequencedTurn(context.Background(), testSequencedTurnWithID(t, sequence, fingerprint, turnID))
	errs <- err
}
