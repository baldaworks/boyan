package scopelanes

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
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
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

	executor, err := NewExecutor(Config{MaxConcurrentScopes: 2, MaxQueuedPerScope: 2})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
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
