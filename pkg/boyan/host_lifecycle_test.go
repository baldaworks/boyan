package boyan

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/normahq/runtime/v2/agentconfig"
	runtimeconfig "github.com/normahq/runtime/v2/appconfig"
)

func TestHostLifecycleUsesRuntimeSeam(t *testing.T) {
	t.Parallel()

	host, err := New(context.Background(), testHostConfig(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if host.Ready() {
		t.Fatal("host.Ready() = true, want false before Start")
	}
	if host.MCP() == nil {
		t.Fatal("host.MCP() = nil, want public MCP surface")
	}

	preStart := httptest.NewRecorder()
	preStartRequest := httptest.NewRequest(http.MethodGet, "http://boyan/readyz", nil)
	host.Handler().ServeHTTP(preStart, preStartRequest)
	if preStart.Code != http.StatusServiceUnavailable {
		t.Fatalf("pre-start readiness status = %d, want %d", preStart.Code, http.StatusServiceUnavailable)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := host.Start(ctx); err != nil {
		t.Fatalf("host.Start() error = %v", err)
	}
	if !host.Ready() {
		t.Fatal("host.Ready() = false, want true after Start")
	}
	if got := host.Addr(); got == "127.0.0.1:0" {
		t.Fatalf("host.Addr() = %q, want bound listener address", got)
	}
	response, err := (&http.Client{Timeout: 2 * time.Second}).Get("http://" + host.Addr() + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz error = %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /readyz status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	waitCtx, waitCancel := context.WithCancel(context.Background())
	waitCancel()
	if err := host.Wait(waitCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("host.Wait(canceled) error = %v, want context cancellation", err)
	}

	if err := host.Close(); err != nil {
		t.Fatalf("host.Close() error = %v", err)
	}
	if host.Ready() {
		t.Fatal("host.Ready() = true, want false after Close")
	}
}

func TestHostRunStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	host, err := New(context.Background(), testHostConfig(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- host.Run(runCtx)
	}()
	waitForHostReady(t, host)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("host.Run() error = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("host.Run() did not stop after context cancellation")
	}
	if host.Ready() {
		t.Fatal("host.Ready() = true after Run returned, want false")
	}
}

func TestHostCapabilitySurface(t *testing.T) {
	t.Parallel()

	host, err := New(context.Background(), testHostConfig(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	scope := memory.Scope{Key: "telegram:host-capabilities", Kind: memory.ScopeKindPersonal}
	turn, err := memory.NewTurn(
		scope,
		memory.ConversationRef{ConversationID: "host-conversation-1", AgentConversationID: "host-agent-1"},
		"host-turn-1",
		time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC),
		"Ada",
		"Ada lives in Bishkek",
	)
	if err != nil {
		t.Fatalf("memory.NewTurn() error = %v", err)
	}
	if _, err := host.IngestSequencedTurn(context.Background(), memory.SequencedTurn{
		ProducerID:    "host-producer-1",
		ScopeSequence: 1,
		Fingerprint:   "host-fingerprint-1",
		Turn:          turn,
	}); err == nil {
		t.Fatal("host.IngestSequencedTurn() error = nil before Start, want not-started failure")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := host.Start(ctx); err != nil {
		t.Fatalf("host.Start() error = %v", err)
	}
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Fatalf("host.Close() error = %v", err)
		}
	})

	recall, err := host.Search(ctx, memory.RecallRequest{
		SchemaVersion: memory.RecallSchemaVersionV1,
		Scope:         scope,
		Query:         "Ada",
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("host.Search() error = %v", err)
	}
	if recall.Scope != scope {
		t.Fatalf("host.Search() scope = %#v, want %#v", recall.Scope, scope)
	}
	if _, err := host.Trace(ctx, memory.TraceRequest{
		SchemaVersion: memory.DerivedSchemaVersionV1,
		Scope:         scope,
		MaxNodes:      10,
	}); err == nil {
		t.Fatal("host.Trace() error = nil, want validation failure for missing root")
	}
	if _, err := host.ForgetScope(ctx, memory.ForgetScopeCommand{}); err == nil {
		t.Fatal("host.ForgetScope() error = nil, want validation failure")
	}
	if host.MCP() == nil {
		t.Fatal("host.MCP() = nil, want public MCP surface")
	}
}

func testHostConfig(t *testing.T) Config {
	t.Helper()

	cfg := DefaultConfig()
	cfg.StateDir = filepath.Join(t.TempDir(), "boyan-state")
	cfg.HTTP.Listen = "127.0.0.1:0"
	cfg.Derivation.Provider = "memory"
	cfg.Derivation.WorkingDir = t.TempDir()
	cfg.Derivation.Runtime = runtimeconfig.RuntimeConfig{
		Providers: map[string]agentconfig.Config{
			"memory": {
				Type:     agentconfig.AgentTypeCodexACP,
				CodexACP: &agentconfig.ACPConfig{Model: "memory-fast"},
			},
		},
	}
	return cfg
}

func waitForHostReady(t *testing.T, host *Host) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if host.Ready() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("host did not become ready")
}
