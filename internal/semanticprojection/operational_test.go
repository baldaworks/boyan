package semanticprojection //nolint:testpackage // Verifies private status ownership and counters with real builds.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

//nolint:cyclop // Cancellation timing cases share the exact status and scheduler ownership assertions.
func TestInvalidateStalePreservesReadyOnCanceledVerification(t *testing.T) {
	for _, mode := range []string{"canceled", "expired", "during-metadata-read"} {
		t.Run(mode, func(t *testing.T) {
			scope := testScope("canceled-verification")
			canonical := newFakeCanonical(scope, 7)
			manifests := &fakeManifestSource{manifest: activeManifest(scope, "generation-7", 7)}
			provider := &fakeProvider{vector: []float32{1, 0}}

			manager := newTestManager(t, canonical, manifests, provider,
				openSemanticStore(t, canonical), testModelSpace("model-a"))
			if err := manager.Repair(context.Background(), scope); err != nil {
				t.Fatal(err)
			}

			previous := manager.statuses[scopeKey(scope)]
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)

			wantErr := context.Canceled

			switch mode {
			case "canceled":
				cancel()
			case "expired":
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				t.Cleanup(cancel)

				wantErr = context.DeadlineExceeded
			case "during-metadata-read":
				canonical.loadStateHook = func(context.Context) { cancel() }
			}

			manager.InvalidateStale(ctx, scope)

			canonical.loadStateHook = nil

			if !errors.Is(ctx.Err(), wantErr) {
				t.Fatalf("verification context error = %v, want %v", ctx.Err(), wantErr)
			}

			status, err := manager.Status(context.Background(), scope)
			if err != nil || status.State != StateReady || manager.OperationalState() != StateReady ||
				manager.statuses[scopeKey(scope)] != previous || manager.degradedCount != 0 ||
				manager.repairingCount != 0 || len(manager.schedules) != 0 || len(manager.queue) != 0 {
				t.Fatalf("inconclusive verification changed ready state: status=%+v err=%v aggregate=%s",
					status, err, manager.OperationalState())
			}
		})
	}
}

//nolint:cyclop // Initial, concurrent invalidation and recovery share one counter lifecycle.
func TestInvalidateStaleIsIdempotentAndOwnsNoNewScopes(t *testing.T) {
	scope := testScope("invalidate")
	canonical := newFakeCanonical(scope, 7)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "generation-7", 7)}

	manager := newTestManager(t, canonical, manifests, &fakeProvider{vector: []float32{1, 0}},
		openSemanticStore(t, canonical), testModelSpace("model-a"))
	if err := manager.Repair(context.Background(), scope); err != nil {
		t.Fatal(err)
	}

	manager.InvalidateStale(context.Background(), scope)

	if manager.OperationalState() != StateReady {
		t.Fatal("unchanged certified view lost readiness")
	}

	canonical.setSequence(8)

	var callers sync.WaitGroup
	for range 32 {
		callers.Go(func() { manager.InvalidateStale(context.Background(), scope) })
	}

	callers.Wait()

	for index := range 128 {
		manager.InvalidateStale(context.Background(), testScope(fmt.Sprint("unknown:", index)))
	}

	if manager.OperationalState() != StateDegraded || manager.degradedCount != 1 || manager.repairingCount != 0 ||
		len(manager.statuses) != 1 || len(manager.schedules) != 0 || len(manager.queue) != 0 {
		t.Fatalf("invalidation counters/ownership: degraded=%d repairing=%d scopes=%d schedules=%d queue=%d",
			manager.degradedCount, manager.repairingCount, len(manager.statuses), len(manager.schedules), len(manager.queue))
	}

	status, err := manager.Status(context.Background(), scope)
	if err != nil || status.State != StateDegraded || status.SafeReason != semanticcore.ReasonUnavailable {
		t.Fatalf("invalidated status: %+v, %v", status, err)
	}

	manifests.set(activeManifest(scope, "generation-8", 8))

	if err := manager.Repair(context.Background(), scope); err != nil {
		t.Fatal(err)
	}

	if manager.OperationalState() != StateReady || manager.degradedCount != 0 || manager.repairingCount != 0 {
		t.Fatal("repair did not balance aggregate counters")
	}
}

func TestInvalidateStalePreservesConcurrentPublication(t *testing.T) {
	scope := testScope("publication")
	canonical := newFakeCanonical(scope, 7)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "generation-7", 7)}

	manager := newTestManager(t, canonical, manifests, &fakeProvider{vector: []float32{1, 0}},
		openSemanticStore(t, canonical), testModelSpace("model-a"))
	if err := manager.Repair(context.Background(), scope); err != nil {
		t.Fatal(err)
	}

	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})

	var calls atomic.Int32

	canonical.loadStateHook = func(context.Context) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
	}

	go func() {
		defer close(done)

		manager.InvalidateStale(context.Background(), scope)
	}()

	<-entered
	canonical.setSequence(8)
	manifests.set(activeManifest(scope, "generation-8", 8))
	err := manager.Repair(context.Background(), scope)

	close(release)
	<-done

	if err != nil {
		t.Fatal(err)
	}

	if manager.OperationalState() != StateReady || manager.degradedCount != 0 || manager.repairingCount != 0 {
		t.Fatal("stale observation overwrote newer ready publication")
	}
}

//nolint:cyclop // Queued, in-flight and complete states share one scheduled repair.
func TestInvalidateStalePreservesQueuedAndInflightRepair(t *testing.T) {
	scope := testScope("scheduled")
	canonical := newFakeCanonical(scope, 7)
	manifests := &fakeManifestSource{manifest: activeManifest(scope, "generation-7", 7)}

	manager := newTestManager(t, canonical, manifests, &fakeProvider{vector: []float32{1, 0}},
		openSemanticStore(t, canonical), testModelSpace("model-a"))
	if err := manager.Repair(context.Background(), scope); err != nil {
		t.Fatal(err)
	}

	canonical.setSequence(8)
	manifests.set(activeManifest(scope, "generation-8", 8))
	// Admit into the real queue before starting workers to inspect both states.
	manager.lifecycleMu.Lock()
	manager.started = true
	manager.lifecycleMu.Unlock()

	if err := manager.Schedule(scope); err != nil {
		t.Fatal(err)
	}

	manager.InvalidateStale(context.Background(), scope)

	if manager.OperationalState() != StateRepairing || manager.repairingCount != 1 || manager.degradedCount != 0 {
		t.Fatal("invalidation replaced queued repair")
	}

	entered, release := make(chan struct{}), make(chan struct{})
	manager.beforeBuild = func(context.Context, memory.Scope) {
		close(entered)
		<-release
	}

	done := make(chan struct{})
	go func() {
		defer close(done)

		manager.runScheduled(context.Background(), <-manager.queue)
	}()

	<-entered
	manager.InvalidateStale(context.Background(), scope)

	if manager.OperationalState() != StateRepairing {
		t.Error("invalidation replaced in-flight repair")
	}

	close(release)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduled repair did not finish")
	}

	if manager.OperationalState() != StateReady || manager.repairingCount != 0 || manager.degradedCount != 0 {
		t.Fatal("scheduled repair did not balance counters")
	}
}
