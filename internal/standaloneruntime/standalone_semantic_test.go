package standaloneruntime //nolint:testpackage // Exercises actual canonical, lexical and semantic runtime ownership.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalcore"
	"github.com/baldaworks/boyan/internal/embedding"
	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/internal/semanticprojection"
	"github.com/baldaworks/boyan/pkg/memory"
)

type lifecycleEmbeddingProvider struct {
	calls   atomic.Int32
	mode    atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (provider *lifecycleEmbeddingProvider) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	provider.calls.Add(1)

	if provider.entered != nil {
		select {
		case provider.entered <- struct{}{}:
		default:
		}

		select {
		case <-provider.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	switch provider.mode.Load() {
	case 1:
		return nil, &embedding.Error{Code: embedding.CodeUnavailable}
	case 2:
		return nil, &embedding.Error{Code: embedding.CodeDeadline}
	case 3:
		return [][]float32{{0, 0}}, nil
	}

	vectors := make([][]float32, len(inputs))
	for i := range vectors {
		vectors[i] = []float32{1, 0}
	}

	return vectors, nil
}

func enabledLifecycleConfig(t *testing.T, provider embedding.Provider) Config {
	t.Helper()
	cfg := testStandaloneConfig(t)
	cfg.Embeddings = EmbeddingsConfig{Provider: provider, ModelSpace: semanticcore.ModelSpace{
		Model: "lifecycle-model", Revision: "immutable", Dimensions: 2, QueryPrefix: "query: ", PassagePrefix: "passage: ",
	}}

	return cfg
}

func seedSemanticTurn(t *testing.T, runtime *Runtime, id string) memory.Turn {
	t.Helper()

	scope := memory.Scope{Kind: memory.ScopeKindPersonal, Key: "standalone:semantic"}

	return seedSemanticScopeTurn(t, runtime, scope, id)
}

func seedSemanticScopeTurn(t *testing.T, runtime *Runtime, scope memory.Scope, id string) memory.Turn {
	t.Helper()
	requireNoError(t, runtime.service.store.PrepareSourcePrivacy(t.Context()), "prepare fixture: %v")

	conversation := memory.ConversationRef{ConversationID: "conversation", AgentConversationID: "agent"}
	turn, err := memory.NewTurn(scope, conversation,
		id, time.Now().UTC().Add(-time.Hour), "Bishkek home "+id, "OK")
	requireNoError(t, err, "turn: %v")
	processor, err := canonicalcore.NewCanonicalTurnProcessor(runtime.service.store, restoreStartupExtractor{},
		memory.PolicyRegistry{Version: "policy-v1"})
	requireNoError(t, err, "processor: %v")
	_, err = processor.ProcessTurn(context.Background(), turn, memory.LegacyDerivationRef())
	requireNoError(t, err, "seed: %v")

	return turn
}

func waitSemanticState(t *testing.T, runtime *Runtime, scope memory.Scope, want string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status, err := runtime.SemanticStatus(context.Background(), scope)
		requireNoError(t, err, "status: %v")

		if status.State == want {
			return
		}

		time.Sleep(time.Millisecond)
	}

	t.Fatalf("semantic status did not reach %s", want)
}

// Removing background repair, passing semantic failures into lexical readiness,
// or failing to schedule post-commit refresh breaks this real lifecycle test.
func TestStandaloneSemanticOutageRepairAndSourceForget(t *testing.T) {
	for _, mode := range []int32{1, 2, 3} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			provider := &lifecycleEmbeddingProvider{}
			provider.mode.Store(mode)
			runtime, err := New(enabledLifecycleConfig(t, provider))
			requireNoError(t, err, "new: %v")
			t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
			turn := seedSemanticTurn(t, runtime, "first")
			requireNoError(t, runtime.Start(context.Background()), "start: %v")
			waitSemanticState(t, runtime, turn.Scope, "degraded")

			if !runtime.Ready() {
				t.Fatal("semantic outage disabled lexical readiness")
			}

			request := memory.RecallRequest{Scope: turn.Scope, Query: "Bishkek", Limit: 10}
			response, err := runtime.Search(context.Background(), request)
			requireNoError(t, err, "fallback: %v")

			if len(response.Results) != 1 || response.Retrieval == nil || response.Retrieval.Effective != "degraded" {
				t.Fatalf("fallback: %+v", response)
			}

			provider.mode.Store(0)

			syncer := semanticSyncer{lexical: runtime.service.projection, manager: runtime.service.semantic.manager}
			requireNoError(t, syncer.Sync(context.Background(), turn.Scope), "post-lexical repair: %v")
			waitSemanticState(t, runtime, turn.Scope, "hybrid-ready")
			// A live canonical update invalidates the prior view before inference finishes.
			second := seedSemanticTurn(t, runtime, "second")
			status, err := runtime.SemanticStatus(context.Background(), turn.Scope)
			requireNoError(t, err, "stale status: %v")

			if status.State == "hybrid-ready" {
				t.Fatal("canonical change left old vectors compatible")
			}

			requireNoError(t, syncer.Sync(context.Background(), second.Scope), "sync update: %v")
			waitSemanticState(t, runtime, turn.Scope, "hybrid-ready")
			_, err = runtime.ForgetSource(context.Background(), memory.ForgetSourceCommand{
				SchemaVersion: memory.DerivedSchemaVersionV1,
				Source: memory.SourceRef{Scope: turn.Scope, ExportID: turn.ExportID,
					ConversationID: turn.Conversation.ConversationID, SourceTurnID: turn.SourceTurnID},
				ForgottenAt: time.Now().UTC(),
			})
			requireNoError(t, err, "forget source: %v")
			response, err = runtime.Search(context.Background(), request)
			requireNoError(t, err, "post-forget recall: %v")

			if len(response.Results) != 1 || response.Results[0].Text != "Bishkek home second" {
				t.Fatalf("forgotten source leaked: %+v", response)
			}
		})
	}
}

func TestStandaloneSemanticShutdownCancelsBlockedInference(t *testing.T) {
	provider := &lifecycleEmbeddingProvider{entered: make(chan struct{}, 1), release: make(chan struct{})}
	cfg := enabledLifecycleConfig(t, provider)
	runtime, err := New(cfg)
	requireNoError(t, err, "new: %v")
	t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
	turn := seedSemanticTurn(t, runtime, "blocked")
	requireNoError(t, runtime.Start(context.Background()), "start: %v")

	select {
	case <-provider.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("inference did not start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	requireNoError(t, runtime.Stop(ctx), "bounded shutdown: %v")
	status, err := runtime.SemanticStatus(context.Background(), turn.Scope)
	requireNoError(t, err, "stopped status: %v")

	if status.State != "stopped" {
		t.Fatalf("status after storage closed: %+v", status)
	}
	// Reopening proves that draining released all three storage owners.
	cfg.Embeddings.Provider = &lifecycleEmbeddingProvider{}
	restarted, err := New(cfg)
	requireNoError(t, err, "reopen: %v")
	requireNoError(t, restarted.Stop(context.Background()), "reopened stop: %v")
}

//nolint:cyclop // Accepted admissions and blocked workers prove capacity without timing assumptions.
func TestSemanticQueueOverflowDoesNotFailLexicalSync(t *testing.T) {
	provider := &lifecycleEmbeddingProvider{entered: make(chan struct{}, 2), release: make(chan struct{})}
	runtime, err := New(enabledLifecycleConfig(t, provider))
	requireNoError(t, err, "new: %v")
	t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
	first := seedSemanticScopeTurn(t, runtime, memory.Scope{Kind: memory.ScopeKindPersonal, Key: "queue:a"}, "one")
	second := seedSemanticScopeTurn(t, runtime, memory.Scope{Kind: memory.ScopeKindPersonal, Key: "queue:b"}, "two")
	requireNoError(t, runtime.Start(context.Background()), "start: %v")

	select {
	case <-provider.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("startup build not blocked")
	}

	requireNoError(t, runtime.service.semantic.manager.Schedule(second.Scope), "second build: %v")

	select {
	case <-provider.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("second build not blocked")
	}
	// Both build slots are occupied. Wait until the remaining worker has
	// consumed its one entry and all 64 queue slots have accepted work.
	accepted := 0

	deadline := time.Now().Add(5 * time.Second)
	for accepted < semanticcore.SemanticQueueLimit+semanticcore.SemanticWorkerLimit-1 {
		if time.Now().After(deadline) {
			t.Fatal("queue did not saturate")
		}

		queued := memory.Scope{Kind: memory.ScopeKindPersonal, Key: fmt.Sprint("queued:", accepted)}

		err := runtime.service.semantic.manager.Schedule(queued)
		if err != nil && !errors.Is(err, semanticprojection.ErrQueueFull) {
			t.Fatalf("queue admission %d: %v", accepted, err)
		}

		if err == nil {
			accepted++
		} else {
			time.Sleep(time.Millisecond)
		}
	}

	syncer := semanticSyncer{lexical: runtime.service.projection, manager: runtime.service.semantic.manager}
	saturated := memory.Scope{Kind: memory.ScopeKindPersonal, Key: "queue:overflow"}
	requireNoError(t, syncer.Sync(context.Background(), saturated), "lexical success under queue overflow: %v")
	status, err := runtime.SemanticStatus(context.Background(), saturated)
	requireNoError(t, err, "status: %v")

	if status.State != "degraded" || status.Reason != "projection_capacity" {
		t.Fatalf("overflow status: %+v", status)
	}

	request := memory.RecallRequest{Scope: first.Scope, Query: "Bishkek", Limit: 10}
	response, err := runtime.Search(context.Background(), request)
	requireNoError(t, err, "lexical recall under saturation: %v")

	if len(response.Results) != 1 {
		t.Fatalf("lexical result lost under saturation: %+v", response)
	}
}

type failingLexicalSyncer struct {
	err    error
	during func()
}

func (syncer failingLexicalSyncer) Sync(context.Context, memory.Scope) error {
	if syncer.during != nil {
		syncer.during()
	}

	return syncer.err
}

func TestSemanticRefreshRequiresLexicalSuccess(t *testing.T) {
	failure := errors.New("lexical failed")
	syncer := semanticSyncer{lexical: failingLexicalSyncer{err: failure}}

	err := syncer.Sync(context.Background(), memory.Scope{Kind: memory.ScopeKindPersonal, Key: "lexical-failure"})
	if !errors.Is(err, failure) {
		t.Fatalf("lexical error=%v", err)
	}
}

func TestCanceledLexicalSyncPreservesHealthySemanticReadiness(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprint("expired=", expired), func(t *testing.T) {
			provider := &lifecycleEmbeddingProvider{}
			runtime, err := New(enabledLifecycleConfig(t, provider))
			requireNoError(t, err, "new: %v")
			t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
			turn := seedSemanticTurn(t, runtime, "first")
			requireNoError(t, runtime.Start(context.Background()), "start: %v")
			waitSemanticState(t, runtime, turn.Scope, "hybrid-ready")
			assertRuntimeSemanticReadiness(t, runtime, "hybrid-ready", "hybrid")

			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			wantErr := context.Canceled

			if expired {
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				t.Cleanup(cancel)

				wantErr = context.DeadlineExceeded
			}

			calls := provider.calls.Load()

			syncer := semanticSyncer{lexical: runtime.service.projection, manager: runtime.service.semantic.manager}
			if err := syncer.Sync(ctx, turn.Scope); !errors.Is(err, wantErr) {
				t.Fatalf("lexical error = %v, want %v", err, wantErr)
			}

			status, err := runtime.SemanticStatus(context.Background(), turn.Scope)
			requireNoError(t, err, "status: %v")

			if status.State != "hybrid-ready" || provider.calls.Load() != calls {
				t.Fatalf("canceled sync changed semantic state: status=%+v calls=%d -> %d",
					status, calls, provider.calls.Load())
			}

			assertRuntimeSemanticReadiness(t, runtime, "hybrid-ready", "hybrid")
		})
	}
}

// A failed lexical update must invalidate the aggregate as well as the exact
// scope, without admitting semantic work. A later successful sync recovers both.
func TestFailedLexicalSyncInvalidatesHealthySemanticReadiness(t *testing.T) {
	provider := &lifecycleEmbeddingProvider{}
	runtime, err := New(enabledLifecycleConfig(t, provider))
	requireNoError(t, err, "new: %v")
	t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
	turn := seedSemanticTurn(t, runtime, "first")
	requireNoError(t, runtime.Start(context.Background()), "start: %v")
	waitSemanticState(t, runtime, turn.Scope, "hybrid-ready")
	assertRuntimeSemanticReadiness(t, runtime, "hybrid-ready", "hybrid")

	seedSemanticTurn(t, runtime, "second")

	calls := provider.calls.Load()
	failure := errors.New("lexical failed")

	syncer := semanticSyncer{lexical: failingLexicalSyncer{err: failure, during: func() {
		assertRuntimeSemanticReadiness(t, runtime, "degraded", "degraded")
	}}, manager: runtime.service.semantic.manager}
	for range 3 {
		if err := syncer.Sync(context.Background(), turn.Scope); !errors.Is(err, failure) {
			t.Fatalf("lexical error = %v", err)
		}
	}

	assertRuntimeSemanticReadiness(t, runtime, "degraded", "degraded")
	waitSemanticState(t, runtime, turn.Scope, "degraded")

	if got := provider.calls.Load(); got != calls {
		t.Fatalf("failed lexical sync invoked provider: calls %d -> %d", calls, got)
	}

	syncer.lexical = runtime.service.projection
	requireNoError(t, syncer.Sync(context.Background(), turn.Scope), "repair sync: %v")
	waitSemanticState(t, runtime, turn.Scope, "hybrid-ready")
	assertRuntimeSemanticReadiness(t, runtime, "hybrid-ready", "hybrid")
}

func assertRuntimeSemanticReadiness(t *testing.T, runtime *Runtime, state, mode string) {
	t.Helper()

	rec := httptest.NewRecorder()
	runtime.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if !runtime.Ready() || rec.Code != http.StatusOK ||
		rec.Header().Get("X-Boyan-Semantic-State") != state ||
		rec.Header().Get("X-Boyan-Retrieval-Mode") != mode || runtime.service.semantic.summary() != state {
		t.Fatalf("readiness: ready=%t code=%d headers=%v summary=%s", runtime.Ready(), rec.Code,
			rec.Header(), runtime.service.semantic.summary())
	}
}
