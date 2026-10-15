package semantic

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestStoreGateDoesNotLetReadersBypassQueuedWriter(t *testing.T) {
	gate := newStoreGate()
	releaseInitial, err := gate.acquire(context.Background(), false)
	internalRequireNoError(t, err)

	events := make(chan string, 2)
	releaseWriter := make(chan struct{})

	go func() {
		release, acquireErr := gate.acquire(context.Background(), true)
		if acquireErr != nil {
			events <- "writer-error"

			return
		}

		events <- "writer"

		<-releaseWriter
		release()
	}()

	// A read deadline can expire while the initial reader still holds the
	// gate only after the writer is queued ahead of it.
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		release, acquireErr := gate.acquire(ctx, false)

		cancel()

		if errors.Is(acquireErr, context.DeadlineExceeded) {
			break
		}

		internalRequireNoError(t, acquireErr)
		release()
	}

	go func() {
		release, acquireErr := gate.acquire(context.Background(), false)
		if acquireErr != nil {
			events <- "reader-error"

			return
		}

		events <- "reader"

		release()
	}()

	releaseInitial()

	if first := <-events; first != "writer" {
		t.Fatalf("first acquisition after queued writer = %q, want writer", first)
	}

	close(releaseWriter)

	if second := <-events; second != "reader" {
		t.Fatalf("second acquisition after writer = %q, want reader", second)
	}
}

func TestStoreGateWriterCompletesUnderSustainedReaders(t *testing.T) {
	gate := newStoreGate()
	stop := make(chan struct{})

	var readers sync.WaitGroup

	for range 16 {
		readers.Add(1)
		go func() {
			defer readers.Done()

			for {
				select {
				case <-stop:
					return
				default:
				}

				release, err := gate.acquire(context.Background(), false)
				if err != nil {
					return
				}

				time.Sleep(100 * time.Microsecond)
				release()
			}
		}()
	}

	t.Cleanup(func() {
		close(stop)
		readers.Wait()
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	release, err := gate.acquire(ctx, true)
	internalRequireNoError(t, err)
	release()
}

func TestStoreGateCanceledWaitDoesNotLeakCapacity(t *testing.T) {
	gate := newStoreGate()
	release, err := gate.acquire(context.Background(), true)
	internalRequireNoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := gate.acquire(ctx, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("acquire(canceled) error = %v, want context.Canceled", err)
	}

	release()

	release, err = gate.acquire(context.Background(), true)
	internalRequireNoError(t, err)
	release()
}
