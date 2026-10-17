package standaloneruntime //nolint:testpackage // Exercises applier within the package.

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

func TestRuntimeFailedStartReleasesStorage(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	requireNoError(t, err, "%v")

	defer listener.Close()

	cfg := testStandaloneConfig(t)
	cfg.HTTP.Listen = listener.Addr().String()

	first, err := New(cfg)
	requireNoError(t, err, "%v")

	defer first.service.store.Close()
	defer first.service.index.Close()

	if err := first.Start(context.Background()); err == nil {
		t.Fatal("Start on occupied address succeeded")
	}

	if first.Ready() {
		t.Fatal("failed Start is ready")
	}

	second, err := New(cfg)
	requireNoError(t, err, "reopen after failed Start: %v")

	defer second.service.store.Close()
	defer second.service.index.Close()
}

func TestRuntimeWaitBroadcast(t *testing.T) {
	runtime, err := New(testStandaloneConfig(t))
	requireNoError(t, err, "%v")

	requireNoError(t, runtime.Start(context.Background()), "%v")
	defer runtime.Stop(context.Background())

	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	if err := runtime.Wait(canceled); err != context.Canceled {
		t.Fatalf("canceled Wait: %v", err)
	}

	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()

	results := make(chan error, 4)

	for range 4 {
		go func() { results <- runtime.Wait(ctx) }()
	}

	requireNoError(t, runtime.Stop(ctx), "%v")

	for range 4 {
		if err := <-results; err != nil {
			t.Errorf("concurrent Wait: %v", err)
		}
	}

	if err := runtime.Wait(ctx); err != nil {
		t.Errorf("repeated Wait: %v", err)
	}
}

func TestRuntimeConcurrentStartStop(t *testing.T) {
	for range 5 {
		runtime, err := New(testStandaloneConfig(t))
		requireNoError(t, err, "%v")

		var group sync.WaitGroup
		group.Go(func() { _ = runtime.Start(context.Background()) })
		group.Go(func() { _ = runtime.Stop(context.Background()) })
		group.Wait()

		requireNoError(t, runtime.Stop(context.Background()), "%v")

		if runtime.Ready() {
			t.Fatal("stopped runtime became ready")
		}
	}
}
