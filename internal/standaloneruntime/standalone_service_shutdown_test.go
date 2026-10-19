package standaloneruntime //nolint:testpackage // Exercises store within the package.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	boyanmcp "github.com/baldaworks/boyan/mcp"
	memory "github.com/baldaworks/boyan/pkg/memory"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRuntimeShutdownTimeoutRetainsResources(t *testing.T) {
	forEachSQLRuntimeConfig(t, testRuntimeShutdownTimeoutRetainsResources)
}

func testRuntimeShutdownTimeoutRetainsResources(t *testing.T, cfg Config) {
	runtime, err := New(cfg)
	requireNoError(t, err, "%v")

	entered, release := make(chan struct{}), make(chan struct{})

	runtime.service.server.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})
	requireNoError(t, runtime.Start(context.Background()), "%v")

	clientDone := make(chan error, 1)

	go func() {
		response, err := http.Get("http://" + runtime.Addr())
		if err == nil {
			_, err = io.Copy(io.Discard, response.Body)
			response.Body.Close()
		}

		clientDone <- err
	}()

	<-entered

	defer func() { close(release); <-clientDone; _ = runtime.Stop(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	started := time.Now()
	err = runtime.Stop(ctx)

	if time.Since(started) > time.Second {
		t.Fatal("Stop exceeded caller deadline")
	}

	code, _, typed := memory.ClassifyError(err)
	if !typed || code != memory.CodeTimeout {
		t.Errorf("Stop error is not typed timeout: %v", err)
	}

	if _, err := runtime.service.store.ScanScopeChanges(
		context.Background(),
		memory.Scope{Kind: memory.ScopeKindPersonal, Key: "test:scope"},
		0,
		1,
	); err != nil {
		t.Errorf("storage closed while handler active: %v", err)
	}

	assertShutdownWaitsForHandler(t, runtime)
}

func TestRuntimeShutdownDrainsEmbeddedOperation(t *testing.T) {
	forEachSQLRuntimeConfig(t, testRuntimeShutdownDrainsEmbeddedOperation)
}

func testRuntimeShutdownDrainsEmbeddedOperation(t *testing.T, cfg Config) {
	runtime, err := New(cfg)
	requireNoError(t, err, "%v")
	requireNoError(t, runtime.Start(context.Background()), "%v")

	operation, done, err := runtime.service.beginOperation(context.Background())
	requireNoError(t, err, "%v")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	if err := runtime.Stop(ctx); err == nil {
		t.Error("Stop reported released active operation")
	}

	if operation.Err() != context.Canceled {
		t.Error("shutdown did not cancel operation")
	}

	if _, _, err := runtime.service.beginOperation(context.Background()); err == nil {
		t.Error("shutdown admitted new operation")
	}

	if _, err := runtime.service.store.ScanScopeChanges(
		context.Background(),
		memory.Scope{Kind: memory.ScopeKindPersonal, Key: "test:scope"},
		0,
		1,
	); err != nil {
		t.Errorf("storage closed while embedded operation active: %v", err)
	}

	done()

	requireNoError(t, runtime.Stop(context.Background()), "%v")

	requireNoError(t, runtime.Wait(context.Background()), "%v")
}

func TestRuntimeEmbeddedSurfacesRejectAfterStop(t *testing.T) {
	runtime, err := New(testStandaloneConfig(t))
	requireNoError(t, err, "%v")

	requireNoError(t, runtime.Stop(context.Background()), "%v")

	response := httptest.NewRecorder()
	runtime.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("stopped Handler status: %d", response.Code)
	}

	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "1"}, nil)
	runtime.MCP().RegisterTools(server)

	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	requireNoError(t, err, "%v")

	defer serverSession.Close()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "1"}, nil)

	session, err := client.Connect(ctx, clientTransport, nil)
	requireNoError(t, err, "%v")

	defer session.Close()

	result, err := session.CallTool(
		ctx,
		&sdkmcp.CallToolParams{
			Name:      boyanmcp.SearchToolName,
			Arguments: map[string]any{"scope_key": "test:scope", "scope_kind": "personal", "query": "test", "limit": 1},
		},
	)
	requireNoError(t, err, "%v")

	encoded, err := json.Marshal(result.StructuredContent)
	requireNoError(t, err, "%v")

	var output boyanmcp.SearchOutput
	requireNoError(t, json.Unmarshal(encoded, &output), "%v")

	if output.Error == nil || output.Error.Code != string(memory.CodeShuttingDown) {
		t.Fatalf("stopped MCP outcome: %s", encoded)
	}
}

func TestRuntimeStoppedIngestPreservesErrorEnvelope(t *testing.T) {
	runtime, err := New(testStandaloneConfig(t))
	requireNoError(t, err, "%v")

	requireNoError(t, runtime.Stop(context.Background()), "%v")

	response := httptest.NewRecorder()
	runtime.Handler().ServeHTTP(
		response,
		httptest.NewRequest(http.MethodPost, "/v1/ingest/turns", strings.NewReader("{}")),
	)

	var outcome memory.IngestErrorResponse
	requireNoError(t, json.Unmarshal(response.Body.Bytes(), &outcome), "%v")

	if response.Code != http.StatusServiceUnavailable ||
		outcome.Code != memory.CodeShuttingDown {
		t.Fatalf("shutdown ingest response: %d %s", response.Code, response.Body.String())
	}
}

func assertShutdownWaitsForHandler(t *testing.T, runtime *Runtime) {
	t.Helper()

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer waitCancel()

	waitStarted := time.Now()

	if err := runtime.Wait(waitCtx); err != context.DeadlineExceeded {
		t.Errorf("Wait reported terminal before handler drained: %v", err)
	}

	if time.Since(waitStarted) < 20*time.Millisecond {
		t.Error("Wait returned before cleanup could finish")
	}
}
