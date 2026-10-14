package scopelanes //nolint:testpackage // Exercises shared private test fixtures within the package.

import (
	"context"
	"sync"
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func TestScopeLaneExecutorSameScopeFIFO(t *testing.T) {
	t.Parallel()

	executor, err := NewExecutor(Config{MaxConcurrentScopes: 2, MaxQueuedPerScope: 4})
	requireNoError(t, err, "NewExecutor() error = %v")

	requireNoError(t, executor.Start(context.Background()), "Start() error = %v")

	t.Cleanup(func() { _ = executor.Close(context.Background()) })

	scope := boyan.Scope{Key: "telegram:scope-1", Kind: boyan.ScopeKindPersonal}

	var mu sync.Mutex

	order := make([]int, 0, 3)
	firstStarted := make(chan struct{})
	release := make(chan struct{})
	run := func(value int) func(context.Context) error {
		return func(context.Context) error {
			mu.Lock()

			order = append(order, value)
			mu.Unlock()

			if value == 1 {
				close(firstStarted)
				<-release
			}

			return nil
		}
	}

	errs := make(chan error, 3)
	go func() { errs <- executor.Execute(context.Background(), scope, run(1)) }()

	<-firstStarted

	go func() { errs <- executor.Execute(context.Background(), scope, run(2)) }()

	waitForLanePending(t, executor, scope, 1)
	go func() { errs <- executor.Execute(context.Background(), scope, run(3)) }()

	waitForLanePending(t, executor, scope, 2)
	close(release)

	for range 3 {
		requireNoError(t, <-errs, "Execute() error = %v")
	}

	mu.Lock()
	defer mu.Unlock()

	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Fatalf("execution order = %v, want [1 2 3]", order)
	}
}

func TestScopeLaneExecutorDifferentScopesRunConcurrently(t *testing.T) {
	t.Parallel()

	executor, err := NewExecutor(Config{MaxConcurrentScopes: 2, MaxQueuedPerScope: 2})
	requireNoError(t, err, "NewExecutor() error = %v")

	requireNoError(t, executor.Start(context.Background()), "Start() error = %v")

	t.Cleanup(func() { _ = executor.Close(context.Background()) })

	firstStarted := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)

	go func() {
		firstDone <- executor.Execute(context.Background(), boyan.Scope{
			Key:  "telegram:scope-a",
			Kind: boyan.ScopeKindPersonal,
		}, func(
			context.Context,
		) error {
			close(firstStarted)
			<-release

			return nil
		})
	}()

	<-firstStarted

	secondStarted := make(chan struct{})

	go func() {
		secondDone <- executor.Execute(context.Background(), boyan.Scope{
			Key:  "telegram:scope-b",
			Kind: boyan.ScopeKindPersonal,
		}, func(
			context.Context,
		) error {
			close(secondStarted)

			return nil
		})
	}()

	select {
	case <-secondStarted:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("second scope did not start concurrently")
	}

	close(release)

	requireNoError(t, <-firstDone, "first Execute() error = %v")

	requireNoError(t, <-secondDone, "second Execute() error = %v")
}

func TestScopeLaneExecutorQueuedCancellationFreesSlot(t *testing.T) {
	executor, err := NewExecutor(Config{MaxConcurrentScopes: 2, MaxQueuedPerScope: 2})
	requireNoError(t, err, "NewExecutor() error = %v")
	requireNoError(t, executor.Start(context.Background()), "Start() error = %v")

	started, release := make(chan struct{}), make(chan struct{})

	var releaseOnce sync.Once

	releaseFirst := func() { releaseOnce.Do(func() { close(release) }) }

	t.Cleanup(func() { releaseFirst(); _ = executor.Close(context.Background()) })

	scope := boyan.Scope{Key: "test:cancel", Kind: boyan.ScopeKindPersonal}
	first := executeAsync(executor, context.Background(), scope, func(context.Context) error {
		close(started)
		<-release

		return nil
	})

	awaitSignal(t, started)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	effects := make(chan int, 3)
	canceled := executeAsync(executor, ctx, scope, func(context.Context) error {
		effects <- 0

		return nil
	})
	waitForLanePending(t, executor, scope, 1)
	survivor := executeAsync(executor, context.Background(), scope, func(context.Context) error {
		effects <- 1

		return nil
	})
	waitForLanePending(t, executor, scope, 2)
	cancel()
	requireTimeout(t, awaitResult(t, canceled))

	replacement := executeAsync(executor, context.Background(), scope, func(context.Context) error {
		effects <- 2

		return nil
	})
	waitForLanePending(t, executor, scope, 2)

	requireOtherScopeProgress(t, executor)

	select {
	case <-first:
		t.Fatal("live predecessor returned before release")
	default:
	}

	closeDone := closeWhileBlocked(t, executor)

	releaseFirst()
	finishQueuedCancellation(t, first, survivor, replacement, closeDone, effects)
}
