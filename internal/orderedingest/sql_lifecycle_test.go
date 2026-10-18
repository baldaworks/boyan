package orderedingest_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	"github.com/baldaworks/boyan/internal/orderedingest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

type sqlProjection struct {
	mu     sync.Mutex
	calls  int
	synced chan struct{}
}

func (p *sqlProjection) Sync(context.Context, memory.Scope) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.calls++
	if p.calls == 1 {
		return memory.RetryableError(memory.CodeStoreFailure, "fixture projection unavailable", nil)
	}

	if p.calls == 2 {
		close(p.synced)
	}

	return nil
}

func TestSQLOrderedProjectionRepairAndClose(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, fixture *storefixture.Fixture) {
		store := fixture.Store.(sqlIngestStore)
		counts := &sqlProcessingCounts{}
		projection := &sqlProjection{synced: make(chan struct{})}
		runtime := newSQLIngestRuntime(t, store, counts, false, projection)
		t.Cleanup(func() { _ = runtime.Close(t.Context()) })
		request := sqlIngestTurn(t)

		accepted, err := runtime.IngestSequencedTurn(t.Context(), request)
		if err != nil || accepted.Projection != memory.ProjectionStatusLagging {
			t.Fatalf("projection-lag outcome=%+v err=%v", accepted, err)
		}

		select {
		case <-projection.synced:
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		case <-time.After(5 * time.Second):
			t.Fatal("existing projection repair did not run")
		}

		assertSQLProjectionReplayAndClose(t, runtime, store, request, counts)
	})
}

func assertSQLProjectionReplayAndClose(t *testing.T, runtime *orderedingest.SequencedIngestRuntime,
	store sqlIngestStore, request memory.SequencedTurn, counts *sqlProcessingCounts) {
	t.Helper()

	duplicate, err := runtime.IngestSequencedTurn(t.Context(), request)
	if err != nil || duplicate.Projection != memory.ProjectionStatusLagging ||
		duplicate.Disposition != memory.IngestDispositionDuplicate {
		t.Fatalf("original lagging receipt changed: %+v err=%v", duplicate, err)
	}

	if err := runtime.Close(t.Context()); err != nil {
		t.Fatal(err)
	}

	_, err = runtime.IngestSequencedTurn(t.Context(), request)
	requireSQLCode(t, err, memory.CodeShuttingDown, memory.ErrorClassPermanent)
	assertSQLCanonicalCount(t, store, request.Turn.Scope, 1)

	if counts.extraction != 1 || counts.turnAttempts != 1 {
		t.Fatalf("projection replay repeated processor/model work=%+v", counts)
	}
}

func TestSQLOrderedSameScopeFIFO(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, fixture *storefixture.Fixture) {
		store := fixture.Store.(sqlIngestStore)
		counts := &sqlProcessingCounts{allowMultiple: true, firstStarted: make(chan struct{}),
			releaseFirst: make(chan struct{})}
		runtime := newSQLIngestRuntime(t, store, counts, false)
		t.Cleanup(func() { _ = runtime.Close(t.Context()) })
		first := sqlIngestTurn(t)

		turn, err := memory.NewTurn(first.Turn.Scope, first.Turn.Conversation, "second", sqlIngestTime(),
			"Ada lives in Bishkek", "OK")
		if err != nil {
			t.Fatal(err)
		}

		second := memory.SequencedTurn{ProducerID: first.ProducerID, ScopeSequence: 2,
			Fingerprint: "second", Turn: turn}
		results := make(chan error, 2)

		go func() {
			_, err := runtime.IngestSequencedTurn(t.Context(), first)
			results <- err
		}()

		<-counts.firstStarted

		go func() {
			_, err := runtime.IngestSequencedTurn(t.Context(), second)
			results <- err
		}()

		close(counts.releaseFirst)

		for range 2 {
			if err := <-results; err != nil {
				t.Fatal(err)
			}
		}

		assertSQLCanonicalCount(t, store, first.Turn.Scope, 2)

		for sequence := uint64(1); sequence <= 2; sequence++ {
			record, found, err := store.LoadIngestSequence(t.Context(), first.ProducerID, first.Turn.Scope, sequence)
			if err != nil || !found || record.Outcome.ScopeVersion != sequence {
				t.Fatalf("FIFO sequence=%d receipt=%+v found=%t err=%v", sequence, record, found, err)
			}
		}

		if counts.extraction != 2 || counts.turnAttempts != 2 {
			t.Fatalf("FIFO processor accounting=%+v", counts)
		}
	})
}
