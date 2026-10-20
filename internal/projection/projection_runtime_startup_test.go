//go:build integration

package projection //nolint:testpackage // Exercises optional startup pagination and lifecycle admission.

import (
	"context"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

type startupScopePager struct {
	memory.CanonicalStore

	scopes []memory.ScopeState
	calls  int
}

func (s *startupScopePager) ListCanonicalScopeStates(
	ctx context.Context, cursor []byte, limit int,
) ([]memory.ScopeState, []byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	s.calls++

	if limit > defaultProjectionBatchSize {
		panic("startup requested unbounded page")
	}

	if len(cursor) == 0 {
		return s.scopes[:1], []byte("next"), nil
	}

	return s.scopes[1:], nil, nil
}

func TestProjectionStartSynchronouslyPagesDurableScopes(t *testing.T) {
	runProjectionAuthorities(t, func(t *testing.T, newCanonical projectionOwnerFactory) {
		ctx := context.Background()
		store := func() projectionTestOwner {
			owner, _ := newCanonical(t)

			return owner
		}()

		t.Cleanup(func() { _ = store.Close() })

		pager := &startupScopePager{CanonicalStore: store, scopes: []memory.ScopeState{
			{SchemaVersion: memory.CanonicalSchemaVersionV1,
				Scope: memory.Scope{Key: "startup:a", Kind: memory.ScopeKindPersonal}},
			{SchemaVersion: memory.CanonicalSchemaVersionV1,
				Scope: memory.Scope{Key: "startup:b", Kind: memory.ScopeKindGroup}},
		}}
		applier := &replayOnlyApplier{}
		runtime, err := NewProjectionRuntime(pager, store, applier, "startup", 1)
		requireNoError(t, err, "%v")
		requireNoError(t, runtime.Start(ctx), "%v")

		if pager.calls != 2 || applier.commits != 2 {
			t.Fatalf("pages=%d commits=%d, want 2 each before Start returns", pager.calls, applier.commits)
		}

		for _, state := range pager.scopes {
			_, found, err := store.LoadActiveProjectionManifest(ctx, state.Scope, "startup")
			requireNoError(t, err, "%v")

			if !found {
				t.Fatal("startup missed scope without first manifest")
			}
		}

		requireNoError(t, runtime.Close(ctx), "%v")

		if err := runtime.Start(ctx); err == nil {
			t.Fatal("Start admitted after Close")
		}
	})
}

type blockingStartupPager struct {
	memory.CanonicalStore

	started chan struct{}
}

func (s *blockingStartupPager) ListCanonicalScopeStates(
	ctx context.Context, _ []byte, _ int,
) ([]memory.ScopeState, []byte, error) {
	close(s.started)
	<-ctx.Done()

	return nil, nil, ctx.Err()
}

func TestProjectionCloseDrainsCanceledStartupEnumeration(t *testing.T) {
	runProjectionAuthorities(t, func(t *testing.T, newCanonical projectionOwnerFactory) {
		store := func() projectionTestOwner {
			owner, _ := newCanonical(t)

			return owner
		}()

		t.Cleanup(func() { _ = store.Close() })

		pager := &blockingStartupPager{CanonicalStore: store, started: make(chan struct{})}
		runtime, err := NewProjectionRuntime(pager, store, &replayOnlyApplier{}, "startup", 1)
		requireNoError(t, err, "%v")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		done := make(chan error, 1)
		go func() { done <- runtime.Start(ctx) }()

		<-pager.started

		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer closeCancel()

		if err := runtime.Close(closeCtx); err == nil {
			t.Fatal("Close ignored active startup enumeration")
		}

		cancel()

		if err := <-done; err == nil {
			t.Fatal("Start ignored cancellation")
		}

		requireNoError(t, runtime.Close(context.Background()), "%v")
	})
}

type failingStartupCheckpoints struct {
	memory.ProjectionCheckpointStore
}

func (failingStartupCheckpoints) LoadActiveProjectionManifest(
	context.Context, memory.Scope, string,
) (memory.ProjectionManifest, bool, error) {
	return memory.ProjectionManifest{}, false,
		memory.RetryableError(memory.CodeStoreFailure, "checkpoint read failed", nil)
}

type resumableStartupApplier struct {
	replayOnlyApplier

	begins int
}

func (a *resumableStartupApplier) BeginProjectionRebuild(context.Context, memory.Scope, string) error {
	a.begins++

	return nil
}

func (*resumableStartupApplier) ResumeProjection(context.Context, memory.Scope, string) (bool, error) {
	return false, nil
}

func TestProjectionStartPropagatesCheckpointFailure(t *testing.T) {
	runProjectionAuthorities(t, func(t *testing.T, newCanonical projectionOwnerFactory) {
		store := func() projectionTestOwner {
			owner, _ := newCanonical(t)

			return owner
		}()

		t.Cleanup(func() { _ = store.Close() })

		pager := &startupScopePager{CanonicalStore: store, scopes: []memory.ScopeState{
			{SchemaVersion: memory.CanonicalSchemaVersionV1,
				Scope: memory.Scope{Key: "startup:error", Kind: memory.ScopeKindPersonal}},
		}}
		applier := &resumableStartupApplier{}
		runtime, err := NewProjectionRuntime(pager, failingStartupCheckpoints{store}, applier, "startup", 1)
		requireNoError(t, err, "%v")

		if err := runtime.Start(context.Background()); err == nil {
			t.Fatal("Start discarded checkpoint read failure")
		}

		if applier.commits != 0 || applier.begins != 0 {
			t.Fatal("failed checkpoint read installed a replacement generation")
		}
	})
}
