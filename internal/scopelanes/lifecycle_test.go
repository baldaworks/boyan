package scopelanes //nolint:testpackage // Exercises shared private test fixtures within the package.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func TestScopeLaneExecutorCloseRejectsNewWorkAndWaitsForDrain(t *testing.T) {
	t.Parallel()

	executor, err := NewExecutor(Config{})
	requireNoError(t, err, "NewExecutor() error = %v")

	requireNoError(t, executor.Start(context.Background()), "Start() error = %v")

	scope := boyan.Scope{Key: "telegram:scope-close", Kind: boyan.ScopeKindPersonal}
	started := make(chan struct{})
	release := make(chan struct{})

	done := make(chan error, 1)
	go func() {
		done <- executor.Execute(context.Background(), scope, func(context.Context) error {
			close(started)
			<-release

			return nil
		})
	}()

	<-started

	closeResult := make(chan error, 1)
	go func() { closeResult <- executor.Close(context.Background()) }()

	time.Sleep(25 * time.Millisecond)

	select {
	case err := <-closeResult:
		t.Fatalf("Close() returned early with %v", err)
	default:
	}

	err = executor.Execute(context.Background(), scope, func(context.Context) error { return nil })

	code, _, ok := boyan.ClassifyError(err)
	if !ok || code != boyan.CodeShuttingDown {
		t.Fatalf(
			"Execute(after close) code = %q, ok=%v; want %q, true",
			code,
			ok,
			boyan.CodeShuttingDown,
		)
	}

	close(release)

	requireNoError(t, <-done, "draining Execute() error = %v")

	requireNoError(t, <-closeResult, "Close() error = %v")
}

func TestScopeLaneExecutorCloseHonorsContext(t *testing.T) {
	t.Parallel()

	executor, err := NewExecutor(Config{})
	requireNoError(t, err, "NewExecutor() error = %v")

	requireNoError(t, executor.Start(context.Background()), "Start() error = %v")

	scope := boyan.Scope{Key: "telegram:scope-timeout", Kind: boyan.ScopeKindPersonal}
	started := make(chan struct{})
	release := make(chan struct{})

	go func() {
		_ = executor.Execute(context.Background(), scope, func(context.Context) error {
			close(started)
			<-release

			return nil
		})
	}()

	<-started

	closeCtx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	err = executor.Close(closeCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close(timeout) error = %v, want deadline exceeded wrapped", err)
	}

	close(release)

	_ = executor.Close(context.Background())
}

func TestScopeLaneExecutorActiveCancellationWaitsForCapturedOutcome(t *testing.T) {
	executor, err := NewExecutor(Config{})
	requireNoError(t, err, "NewExecutor() error = %v")
	requireNoError(t, executor.Start(context.Background()), "Start() error = %v")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	release := make(chan struct{})

	var once sync.Once

	finish := func() { once.Do(func() { close(release) }) }

	t.Cleanup(func() { finish(); _ = executor.Close(context.Background()) })

	outcome := ""
	expected := errors.New("callback result")
	scope := boyan.Scope{Key: "test:active", Kind: boyan.ScopeKindPersonal}
	done := executeAsync(executor, ctx, scope, func(context.Context) error {
		close(started)
		<-release

		outcome = "completed"

		return expected
	})

	awaitSignal(t, started)
	cancel()

	select {
	case err := <-done:
		t.Fatalf("active caller returned before captured outcome: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	finish()

	if err := awaitResult(t, done); !errors.Is(err, expected) {
		t.Fatalf("active result = %v, want callback result", err)
	}

	if outcome != "completed" {
		t.Fatalf("captured outcome = %q", outcome)
	}
}

func TestScopeLaneWorkerRejectsCanceledContextAfterSemaphoreAcquisition(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// The semaphore may be selected even when ctx.Done is already ready.
	// Exercise its callback boundary directly so select scheduling cannot hide it.
	called := false
	err := runOwnedScopeLaneWork(scopeLaneWork{ctx: ctx, run: func(context.Context) error {
		called = true

		return nil
	}})
	requireTimeout(t, err)

	if called {
		t.Fatal("canceled worker-owned callback ran")
	}
}

func TestScopeLaneExecutorWorkerOwnedCancellationWhileSemaphoreFull(t *testing.T) {
	executor, err := NewExecutor(Config{MaxConcurrentScopes: 1, MaxQueuedPerScope: 1})
	requireNoError(t, err, "NewExecutor() error = %v")
	requireNoError(t, executor.Start(context.Background()), "Start() error = %v")

	started, release := make(chan struct{}), make(chan struct{})

	t.Cleanup(func() { close(release); _ = executor.Close(context.Background()) })

	firstScope := boyan.Scope{Key: "test:semaphore-first", Kind: boyan.ScopeKindPersonal}
	first := executeAsync(executor, context.Background(), firstScope, func(context.Context) error {
		close(started)
		<-release

		return nil
	})

	awaitSignal(t, started)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	secondScope := boyan.Scope{Key: "test:semaphore-second", Kind: boyan.ScopeKindPersonal}
	called := make(chan struct{}, 1)
	second := executeAsync(executor, ctx, secondScope, func(context.Context) error {
		called <- struct{}{}

		return nil
	})
	waitForWorkerOwnership(t, executor, secondScope)
	cancel()
	requireTimeout(t, awaitResult(t, second))

	select {
	case <-called:
		t.Fatal("canceled semaphore waiter invoked callback")
	default:
	}

	select {
	case <-first:
		t.Fatal("semaphore owner returned before release")
	default:
	}
}
