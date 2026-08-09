package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/baldaworks/boyan"
)

func TestScopeLaneExecutorSameScopeFIFO(t *testing.T) {
	t.Parallel()

	executor, err := newScopeLaneExecutor(ScopeLaneConfig{MaxConcurrentScopes: 2, MaxQueuedPerScope: 4})
	if err != nil {
		t.Fatalf("newScopeLaneExecutor() error = %v", err)
	}
	if err := executor.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
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
		if err := <-errs; err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Fatalf("execution order = %v, want [1 2 3]", order)
	}
}

func TestScopeLaneExecutorDifferentScopesRunConcurrently(t *testing.T) {
	t.Parallel()

	executor, err := newScopeLaneExecutor(ScopeLaneConfig{MaxConcurrentScopes: 2, MaxQueuedPerScope: 2})
	if err != nil {
		t.Fatalf("newScopeLaneExecutor() error = %v", err)
	}
	if err := executor.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = executor.Close(context.Background()) })

	firstStarted := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)

	go func() {
		firstDone <- executor.Execute(context.Background(), boyan.Scope{Key: "telegram:scope-a", Kind: boyan.ScopeKindPersonal}, func(context.Context) error {
			close(firstStarted)
			<-release
			return nil
		})
	}()
	<-firstStarted

	secondStarted := make(chan struct{})
	go func() {
		secondDone <- executor.Execute(context.Background(), boyan.Scope{Key: "telegram:scope-b", Kind: boyan.ScopeKindPersonal}, func(context.Context) error {
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
	if err := <-firstDone; err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}
}

func TestScopeLaneExecutorRejectsQueueOverflow(t *testing.T) {
	t.Parallel()

	executor, err := newScopeLaneExecutor(ScopeLaneConfig{MaxConcurrentScopes: 1, MaxQueuedPerScope: 1})
	if err != nil {
		t.Fatalf("newScopeLaneExecutor() error = %v", err)
	}
	if err := executor.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = executor.Close(context.Background()) })

	scope := boyan.Scope{Key: "telegram:scope-overflow", Kind: boyan.ScopeKindPersonal}
	release := make(chan struct{})
	firstStarted := make(chan struct{})
	go func() {
		_ = executor.Execute(context.Background(), scope, func(context.Context) error {
			close(firstStarted)
			<-release
			return nil
		})
	}()
	<-firstStarted

	secondQueued := make(chan struct{})
	go func() {
		_ = executor.Execute(context.Background(), scope, func(context.Context) error {
			close(secondQueued)
			return nil
		})
	}()
	select {
	case <-secondQueued:
		t.Fatal("second queued work should not have run before release")
	case <-time.After(25 * time.Millisecond):
	}

	err = executor.Execute(context.Background(), scope, func(context.Context) error { return nil })
	code, class, ok := boyan.ClassifyError(err)
	if !ok || code != boyan.CodeLimitExceeded || class != boyan.ErrorClassRetryable {
		t.Fatalf("queue overflow error = (%q, %q, %v), want (%q, %q, true)", code, class, ok, boyan.CodeLimitExceeded, boyan.ErrorClassRetryable)
	}
	close(release)
}

func TestScopeLaneExecutorCloseRejectsNewWorkAndWaitsForDrain(t *testing.T) {
	t.Parallel()

	executor, err := newScopeLaneExecutor(ScopeLaneConfig{})
	if err != nil {
		t.Fatalf("newScopeLaneExecutor() error = %v", err)
	}
	if err := executor.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

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
		t.Fatalf("Execute(after close) code = %q, ok=%v; want %q, true", code, ok, boyan.CodeShuttingDown)
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("draining Execute() error = %v", err)
	}
	if err := <-closeResult; err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestScopeLaneExecutorRequiresStartedState(t *testing.T) {
	t.Parallel()

	executor, err := newScopeLaneExecutor(ScopeLaneConfig{})
	if err != nil {
		t.Fatalf("newScopeLaneExecutor() error = %v", err)
	}
	err = executor.Execute(context.Background(), boyan.Scope{Key: "telegram:scope-start", Kind: boyan.ScopeKindPersonal}, func(context.Context) error { return nil })
	code, _, ok := boyan.ClassifyError(err)
	if !ok || code != boyan.CodeDisabled {
		t.Fatalf("Execute(before start) code = %q, ok=%v; want %q, true", code, ok, boyan.CodeDisabled)
	}
}

func TestScopeLaneExecutorCloseHonorsContext(t *testing.T) {
	t.Parallel()

	executor, err := newScopeLaneExecutor(ScopeLaneConfig{})
	if err != nil {
		t.Fatalf("newScopeLaneExecutor() error = %v", err)
	}
	if err := executor.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

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

func waitForLanePending(t *testing.T, executor *scopeLaneExecutor, scope boyan.Scope, want int) {
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
