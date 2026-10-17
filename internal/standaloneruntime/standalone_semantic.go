package standaloneruntime

import (
	"bytes"
	"context"
	"sync"
	"sync/atomic"

	"github.com/baldaworks/boyan/index/sqlite"
	"github.com/baldaworks/boyan/internal/embedding"
	"github.com/baldaworks/boyan/internal/projection"
	"github.com/baldaworks/boyan/internal/semanticprojection"
	"github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/badger"
)

const semanticScopePageSize = 128

type semanticRuntime struct {
	manager       *semanticprojection.Manager
	canonical     *badger.CanonicalStore
	provider      *semanticHealthProvider
	startup       sync.WaitGroup
	repairing     atomic.Bool
	startupFailed atomic.Bool
}

func (storage *standaloneStorage) openSemantic(config EmbeddingsConfig,
	cleanups *standaloneCleanupStack) error {
	provider := &semanticHealthProvider{Provider: config.Provider}

	manager, err := semanticprojection.New(semanticprojection.Dependencies{
		Canonical: storage.store, Records: storage.reader, Manifests: storage.store,
		Physical: storage.index, Provider: provider, Store: storage.index, ModelSpace: config.ModelSpace,
		LexicalProjectionID: boyanProjectionID, Lifecycle: storage.index,
	})
	if err != nil {
		return err
	}

	cleanups.Add(func() { _ = manager.Close(context.Background()) })

	hybrid, err := semanticprojection.NewHybrid(semanticprojection.HybridDependencies{
		Canonical: storage.store, Manifests: storage.store, Reader: storage.index,
		Lexical: storage.index, Provider: provider, ModelSpace: config.ModelSpace,
	})
	if err != nil {
		return err
	}

	runtime := &semanticRuntime{manager: manager, canonical: storage.store, provider: provider}
	runtime.repairing.Store(true)
	storage.semantic = runtime
	storage.syncer = semanticSyncer{lexical: storage.projection, manager: manager}
	storage.recallProjection = hybrid

	return nil
}

type semanticSyncer struct {
	lexical projection.Syncer
	manager *semanticprojection.Manager
}

func (syncer semanticSyncer) Sync(ctx context.Context, scope memory.Scope) error {
	syncer.manager.InvalidateStale(ctx, scope)
	err := syncer.lexical.Sync(ctx, scope)
	syncer.manager.InvalidateStale(ctx, scope)

	if err != nil {
		return err
	}
	// Exact canonical/physical compatibility invalidates the old semantic view
	// before this best-effort admission. Queue failure cannot fail lexical work.
	_ = syncer.manager.Schedule(scope)

	return nil
}

func (runtime *semanticRuntime) start(ctx context.Context) {
	runtime.startup.Add(1)
	go func() {
		defer runtime.startup.Done()
		defer runtime.repairing.Store(false)

		if err := runtime.manager.Start(ctx); err != nil {
			// Manager owns classified initialization failure and clears it only
			// after a successful retry through its admission barrier.
			return
		}

		runtime.repairScopes(ctx)
	}()
}

func (runtime *semanticRuntime) repairScopes(ctx context.Context) {
	var cursor []byte
	for ctx.Err() == nil {
		states, next, err := runtime.canonical.ListCanonicalScopeStates(ctx, cursor, semanticScopePageSize)
		if err != nil || len(states) > semanticScopePageSize || len(next) > 0 && bytes.Compare(next, cursor) <= 0 {
			runtime.startupFailed.Store(true)

			return
		}

		for _, state := range states {
			// Synchronous repair in this background pager avoids silently dropping
			// durable scopes when the bounded refresh queue is saturated.
			_ = runtime.manager.Repair(ctx, state.Scope)
			if ctx.Err() != nil {
				return
			}
		}

		if len(next) == 0 {
			return
		}

		cursor = next
	}
}

func (runtime *semanticRuntime) close(ctx context.Context) error {
	if err := runtime.manager.Close(ctx); err != nil {
		return err
	}
	// The service cancels its lifetime context before draining components.
	runtime.startup.Wait()

	return nil
}

var _ semanticprojection.BuildStore = (*sqlite.Store)(nil)
var _ semanticprojection.CandidateLifecycle = (*sqlite.Store)(nil)
var _ semanticprojection.SearchReader = (*sqlite.Store)(nil)

func (runtime *semanticRuntime) summary() string {
	state := runtime.manager.OperationalState()
	if state == semanticprojection.StateStopped {
		return "stopped"
	}

	if runtime.repairing.Load() {
		return "repairing"
	}

	if runtime.startupFailed.Load() || runtime.provider.failed.Load() || state == semanticprojection.StateDegraded {
		return "degraded"
	}

	if state == semanticprojection.StateRepairing {
		return "repairing"
	}

	return "hybrid-ready"
}

// A query outage changes operational availability, never the stored certificate.
type semanticHealthProvider struct {
	embedding.Provider

	failed atomic.Bool
}

func (provider *semanticHealthProvider) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	vectors, err := provider.Provider.Embed(ctx, inputs)
	if ctx.Err() == nil {
		provider.failed.Store(err != nil)
	}

	return vectors, err
}
