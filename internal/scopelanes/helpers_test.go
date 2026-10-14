package scopelanes //nolint:testpackage // Exercises mu within the package.

import (
	"context"
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func waitForLanePending(t *testing.T, executor *Executor, scope boyan.Scope, want int) {
	t.Helper()

	key := string(scope.Kind) + "\x00" + scope.Key

	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		executor.mu.Lock()
		lane := executor.lanes[key]

		got := 0
		if lane != nil {
			got = len(lane.pending)
		}
		executor.mu.Unlock()

		if got >= want {
			return
		}

		time.Sleep(time.Millisecond)
	}

	t.Fatalf("lane pending count did not reach %d", want)
}

func executeAsync(executor *Executor, ctx context.Context, scope boyan.Scope,
	run func(context.Context) error,
) <-chan error {
	done := make(chan error, 1)
	go func() { done <- executor.Execute(ctx, scope, run) }()

	return done
}

func awaitSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()

	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("signal did not arrive")
	}
}

func awaitResult(t *testing.T, done <-chan error) error {
	t.Helper()

	select {
	case err := <-done:
		return err
	case <-time.After(time.Second):
		t.Fatal("Execute did not return while predecessor remained blocked")

		return nil
	}
}

func requireTimeout(t *testing.T, err error) {
	t.Helper()

	code, class, ok := boyan.ClassifyError(err)
	if !ok || code != boyan.CodeTimeout || class != boyan.ErrorClassRetryable {
		t.Fatalf("cancellation = %v, want retryable timeout", err)
	}
}

func finishQueuedCancellation(t *testing.T, first, survivor, replacement, closeDone <-chan error, effects <-chan int) {
	t.Helper()

	for _, done := range []<-chan error{first, survivor, replacement, closeDone} {
		requireNoError(t, awaitResult(t, done), "drain error = %v")
	}

	for _, want := range []int{1, 2} {
		select {
		case got := <-effects:
			if got != want {
				t.Fatalf("effect = %d, want %d", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("survivor did not run")
		}
	}

	select {
	case got := <-effects:
		t.Fatalf("unexpected canceled effect = %d", got)
	default:
	}
}

func closeWhileBlocked(t *testing.T, executor *Executor) <-chan error {
	t.Helper()

	done := make(chan error, 1)
	go func() { done <- executor.Close(context.Background()) }()

	select {
	case err := <-done:
		t.Fatalf("Close returned before drain: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	return done
}

func waitForWorkerOwnership(t *testing.T, executor *Executor, scope boyan.Scope) {
	t.Helper()

	timeout := time.NewTimer(time.Second)
	defer timeout.Stop()

	for {
		executor.mu.Lock()
		lane := executor.lanes[scopeLaneKey(scope)]
		owned := lane != nil && lane.running && len(lane.pending) == 0
		changed := executor.laneChanged
		executor.mu.Unlock()

		if owned {
			return
		}

		select {
		case <-changed:
		case <-timeout.C:
			t.Fatal("worker did not take ownership")
		}
	}
}

func requireOtherScopeProgress(t *testing.T, executor *Executor) {
	t.Helper()

	other := boyan.Scope{Key: "test:other", Kind: boyan.ScopeKindPersonal}
	done := executeAsync(executor, context.Background(), other, func(context.Context) error { return nil })
	requireNoError(t, awaitResult(t, done), "other scope = %v")
}
