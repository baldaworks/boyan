package scopelanes

import (
	"context"
	"errors"
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func TestScopeLaneExecutorCloseRejectsNewWorkAndWaitsForDrain(t *testing.T) {
	t.Parallel()

	executor, err := NewExecutor(Config{})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
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

func TestScopeLaneExecutorCloseHonorsContext(t *testing.T) {
	t.Parallel()

	executor, err := NewExecutor(Config{})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
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
