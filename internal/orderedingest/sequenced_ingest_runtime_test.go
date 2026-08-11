package orderedingest

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
