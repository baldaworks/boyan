//go:build integration

package boyan_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/boyan"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestHostLifecycleUsesRuntimeSeam(t *testing.T) {
	t.Parallel()

	host, err := boyan.New(context.Background(), testHostConfig(t))
	requireNoError(t, err, "New() error = %v")

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
		t.Fatalf(
			"pre-start readiness status = %d, want %d",
			preStart.Code,
			http.StatusServiceUnavailable,
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	requireNoError(t, host.Start(ctx), "host.Start() error = %v")

	if !host.Ready() {
		t.Fatal("host.Ready() = false, want true after Start")
	}

	if got := host.Addr(); got == "127.0.0.1:0" {
		t.Fatalf("host.Addr() = %q, want bound listener address", got)
	}

	response, err := (&http.Client{Timeout: 2 * time.Second}).Get("http://" + host.Addr() + "/readyz")
	requireNoError(t, err, "GET /readyz error = %v")

	response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /readyz status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	waitCtx, waitCancel := context.WithCancel(context.Background())
	waitCancel()

	if err := host.Wait(waitCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("host.Wait(canceled) error = %v, want context cancellation", err)
	}

	requireNoError(t, host.Close(), "host.Close() error = %v")

	if host.Ready() {
		t.Fatal("host.Ready() = true, want false after Close")
	}
}

func TestHostRunStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	host, err := boyan.New(context.Background(), testHostConfig(t))
	requireNoError(t, err, "New() error = %v")

	runCtx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- host.Run(runCtx)
	}()

	waitForHostReady(t, host)
	cancel()

	select {
	case err := <-done:
		requireNoError(t, err, "host.Run() error = %v")
	case <-time.After(10 * time.Second):
		t.Fatal("host.Run() did not stop after context cancellation")
	}

	if host.Ready() {
		t.Fatal("host.Ready() = true after Run returned, want false")
	}
}

func TestHostCapabilitySurface(t *testing.T) {
	t.Parallel()

	host, err := boyan.New(context.Background(), testHostConfig(t))
	requireNoError(t, err, "New() error = %v")

	scope := memory.Scope{Key: "telegram:host-capabilities", Kind: memory.ScopeKindPersonal}

	turn := capabilityTestTurn(t, scope)

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

	requireNoError(t, host.Start(ctx), "host.Start() error = %v")

	t.Cleanup(func() {
		requireNoError(t, host.Close(), "host.Close() error = %v")
	})

	recall, err := host.Search(ctx, memory.RecallRequest{
		SchemaVersion: memory.RecallSchemaVersionV1,
		Scope:         scope,
		Query:         "Ada",
		Limit:         10,
	})
	requireNoError(t, err, "host.Search() error = %v")

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

func TestHostCapabilitiesRejectBeforeStartWithoutMutation(t *testing.T) {
	host, err := boyan.New(context.Background(), testHostConfig(t))
	requireNoError(t, err, "New() error = %v")
	t.Cleanup(func() { _ = host.Close() })

	scope := memory.Scope{Key: "test:prestart-capabilities", Kind: memory.ScopeKindPersonal}
	forgottenAt := time.Date(2026, time.October, 14, 12, 0, 0, 0, time.UTC)
	operations := preStartHostOperations(t, host, scope, forgottenAt)

	assertHostOperationsUnavailable(t, operations)
	assertPreStartHealth(t, host)
	assertFirstPostStartForget(t, host, scope, forgottenAt)
}

type namedHostOperation struct {
	name string
	call func() error
}

func preStartHostOperations(
	t *testing.T,
	host *boyan.Host,
	scope memory.Scope,
	forgottenAt time.Time,
) []namedHostOperation {
	t.Helper()

	turn := capabilityTestTurn(t, scope)

	return []namedHostOperation{
		{name: "ingest turn", call: func() error {
			_, err := host.IngestSequencedTurn(context.Background(), memory.SequencedTurn{
				ProducerID: "prestart-producer", ScopeSequence: 1,
				Fingerprint: "prestart-fingerprint", Turn: turn,
			})

			return err
		}},
		{name: "ingest boundary", call: func() error {
			_, err := host.ApplySequencedBoundary(context.Background(), memory.SequencedBoundary{})

			return err
		}},
		{name: "search", call: func() error {
			_, err := host.Search(context.Background(), memory.RecallRequest{
				SchemaVersion: memory.RecallSchemaVersionV1, Scope: scope, Query: "memory", Limit: 1,
			})

			return err
		}},
		{name: "trace", call: func() error {
			_, err := host.Trace(context.Background(), memory.TraceRequest{
				SchemaVersion: memory.DerivedSchemaVersionV1, Scope: scope,
				Root: memory.RevisionRef{ItemID: "item-1", RevisionID: "revision-1"}, MaxNodes: 1,
			})

			return err
		}},
		{name: "forget source", call: func() error {
			_, err := host.ForgetSource(context.Background(), memory.ForgetSourceCommand{
				SchemaVersion: memory.DerivedSchemaVersionV1,
				Source: memory.SourceRef{
					Scope: scope, ExportID: "export-1", ConversationID: "conversation-1", SourceTurnID: "turn-1",
				},
				ForgottenAt: forgottenAt,
			})

			return err
		}},
		{name: "forget scope", call: func() error {
			_, err := host.ForgetScope(context.Background(), memory.ForgetScopeCommand{
				SchemaVersion: memory.DerivedSchemaVersionV1, Scope: scope,
				RequestID: "prestart-forget", ForgottenAt: forgottenAt,
			})

			return err
		}},
	}
}

func assertHostOperationsUnavailable(t *testing.T, operations []namedHostOperation) {
	t.Helper()

	for _, operation := range operations {
		err := operation.call()
		code, class, ok := memory.ClassifyError(err)

		if !ok || code != memory.CodeUnavailable || class != memory.ErrorClassRetryable {
			t.Errorf("%s error = %v, want retryable unavailable", operation.name, err)
		}
	}
}

func assertPreStartHealth(t *testing.T, host *boyan.Host) {
	t.Helper()

	for _, health := range []struct {
		path string
		want int
	}{{path: "/livez", want: http.StatusOK}, {path: "/readyz", want: http.StatusServiceUnavailable}} {
		response := httptest.NewRecorder()
		host.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, health.path, nil))

		if response.Code != health.want {
			t.Errorf("GET %s status = %d, want %d", health.path, response.Code, health.want)
		}
	}
}

func assertFirstPostStartForget(
	t *testing.T,
	host *boyan.Host,
	scope memory.Scope,
	forgottenAt time.Time,
) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	requireNoError(t, host.Start(ctx), "host.Start() error = %v")

	outcome, err := host.ForgetScope(ctx, memory.ForgetScopeCommand{
		SchemaVersion: memory.DerivedSchemaVersionV1, Scope: scope,
		RequestID: "poststart-forget", ForgottenAt: forgottenAt,
	})
	requireNoError(t, err, "post-start forget error = %v")

	if outcome.ScopeVersion != 1 {
		t.Fatalf("post-start scope version = %d, want 1 with no pre-start mutation", outcome.ScopeVersion)
	}
}

func waitForHostReady(t *testing.T, host *boyan.Host) {
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

func capabilityTestTurn(t *testing.T, scope memory.Scope) memory.Turn {
	t.Helper()

	turn, err := memory.NewTurn(
		scope,
		memory.ConversationRef{ConversationID: "host-conversation-1", AgentConversationID: "host-agent-1"},
		"host-turn-1",
		time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC),
		"Ada",
		"Ada lives in Bishkek",
	)
	requireNoError(t, err, "memory.NewTurn() error = %v")

	return turn
}
