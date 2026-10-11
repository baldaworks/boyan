package scopelanes //nolint:testpackage // Exercises shared private test fixtures within the package.

import (
	"context"
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func TestScopeLaneExecutorRejectsQueueOverflow(t *testing.T) {
	t.Parallel()

	executor, err := NewExecutor(Config{MaxConcurrentScopes: 1, MaxQueuedPerScope: 1})
	requireNoError(t, err, "NewExecutor() error = %v")

	requireNoError(t, executor.Start(context.Background()), "Start() error = %v")

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
		t.Fatalf(
			"queue overflow error = (%q, %q, %v), want (%q, %q, true)",
			code,
			class,
			ok,
			boyan.CodeLimitExceeded,
			boyan.ErrorClassRetryable,
		)
	}

	close(release)
}

func TestScopeLaneExecutorRequiresStartedState(t *testing.T) {
	t.Parallel()

	executor, err := NewExecutor(Config{})
	requireNoError(t, err, "NewExecutor() error = %v")

	err = executor.Execute(
		context.Background(),
		boyan.Scope{Key: "telegram:scope-start", Kind: boyan.ScopeKindPersonal},
		func(context.Context) error { return nil },
	)

	code, _, ok := boyan.ClassifyError(err)
	if !ok || code != boyan.CodeDisabled {
		t.Fatalf(
			"Execute(before start) code = %q, ok=%v; want %q, true",
			code,
			ok,
			boyan.CodeDisabled,
		)
	}
}
