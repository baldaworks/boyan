//go:build integration

package standaloneruntime //nolint:testpackage // Verifies shutdown cancellation reaches synchronous startup repair.

import (
	"context"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/projection"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

type blockedStartupScopeStore struct {
	memory.CanonicalStore

	entered chan struct{}
}

func (s *blockedStartupScopeStore) ListCanonicalScopeStates(
	ctx context.Context, _ []byte, _ int,
) ([]memory.ScopeState, []byte, error) {
	close(s.entered)
	<-ctx.Done()

	return nil, nil, ctx.Err()
}

func TestStopCancelsBlockedProjectionStartup(t *testing.T) {
	forEachSQLRuntimeConfig(t, testStopCancelsBlockedProjectionStartup)
}

func testStopCancelsBlockedProjectionStartup(t *testing.T, cfg Config) {
	runtime, err := New(cfg)
	requireNoError(t, err, "%v")

	pager := &blockedStartupScopeStore{CanonicalStore: runtime.service.store, entered: make(chan struct{})}
	runtime.service.projection, err = projection.NewProjectionRuntime(
		pager, runtime.service.store, runtime.service.applier, "startup-cancel", 1)
	requireNoError(t, err, "%v")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer runtime.Stop(context.Background())

	started := make(chan error, 1)
	go func() { started <- runtime.Start(ctx) }()

	<-pager.entered

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()

	stopErr := runtime.Stop(stopCtx)

	cancel()

	if stopErr != nil {
		t.Fatalf("Stop did not cancel blocked startup: %v", stopErr)
	}

	if err := <-started; err == nil {
		t.Fatal("canceled startup succeeded")
	}

	if runtime.Ready() {
		t.Fatal("stopped startup became ready")
	}
}
