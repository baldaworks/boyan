//go:build integration

package standaloneruntime //nolint:testpackage // Exercises standaloneHTTPConfig within the package.

import (
	"bufio"
	"context"
	"fmt"
	boyanmcp "github.com/baldaworks/boyan/mcp"
	memory "github.com/baldaworks/boyan/pkg/memory"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func limitedHTTPServer(t *testing.T, timeout time.Duration, limit int64) *httptest.Server {
	t.Helper()
	cfg := testStandaloneConfig(t)
	cfg.HTTP.RequestTimeout = timeout
	cfg.HTTP.MaxBodyBytes = limit

	runtime, err := New(cfg)
	requireNoError(t, err, "%v")

	requireNoError(t, runtime.Start(context.Background()), "%v")

	t.Cleanup(func() {
		if err := runtime.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})

	testServer := httptest.NewServer(runtime.Handler())
	t.Cleanup(testServer.Close)

	return testServer
}

func TestHTTPStalledUpload(t *testing.T) {
	for _, path := range []string{"/v1/ingest/turns", "/mcp"} {
		t.Run(path, func(t *testing.T) {
			server := limitedHTTPServer(t, 30*time.Millisecond, 1024)

			conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
			requireNoError(t, err, "%v")

			defer conn.Close()

			if _, err := fmt.Fprintf(
				conn,
				("POST %s HTTP/1.1\r\nHost: localhost\r\nContent-Type: application" +
					"/json\r\nAccept: application/json, text/event-stream\r\nContent-" +
					"Length: 1000\r\n\r\n{"),
				path,
			); err != nil {
				t.Fatal(err)
			}

			requireNoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)), "%v")

			response, err := http.ReadResponse(bufio.NewReader(conn), nil)
			requireNoError(t, err, "stalled upload did not terminate: %v")

			defer response.Body.Close()

			if response.StatusCode < 400 {
				t.Fatalf("stalled upload status: %d", response.StatusCode)
			}
		})
	}
}

func TestHTTPOversizedMCP(t *testing.T) {
	server := limitedHTTPServer(t, time.Second, 32)

	request, err := http.NewRequest(
		http.MethodPost,
		server.URL+"/mcp",
		strings.NewReader(
			("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"pro" +
				"tocolVersion\":\"2025-03-26\",\"capabilities\":{},\"clientInfo\":{\"" +
				"name\":\"test\",\"version\":\"1\"}}}"),
		),
	)
	requireNoError(t, err, "%v")

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")

	response, err := server.Client().Do(request)
	requireNoError(t, err, "%v")

	defer response.Body.Close()

	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatal(err)
	}

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("oversized MCP status: %d", response.StatusCode)
	}

	if response.Header.Get("Mcp-Session-Id") != "" {
		t.Fatal("oversized initialize created a session")
	}
}

type uploadSearchSpy struct{ calls atomic.Int32 }

func (s *uploadSearchSpy) Search(_ context.Context, _ memory.RecallRequest) (memory.RecallResponse, error) {
	s.calls.Add(1)

	return memory.RecallResponse{}, nil
}

func TestHTTPOversizedMCPDoesNotExecuteTool(t *testing.T) {
	spy := &uploadSearchSpy{}

	handler, err := boyanmcp.NewHandler(boyanmcp.Config{Enabled: true, Searcher: spy})
	requireNoError(t, err, "%v")

	server := httptest.NewServer(
		httpBodyLimits(standaloneHTTPConfig{RequestTimeout: time.Second, MaxBodyBytes: 512}, handler),
	)
	defer server.Close()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "1"}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	session, err := client.Connect(ctx, &sdkmcp.StreamableClientTransport{Endpoint: server.URL}, nil)
	requireNoError(t, err, "%v")

	defer session.Close()

	_, err = session.CallTool(
		ctx,
		&sdkmcp.CallToolParams{
			Name: boyanmcp.SearchToolName,
			Arguments: map[string]any{
				"scope_key":  "test:scope",
				"scope_kind": "personal",
				"query":      strings.Repeat("x", 1024),
				"limit":      1,
			},
		},
	)
	if err == nil {
		t.Fatal("oversized tool call succeeded")
	}

	if spy.calls.Load() != 0 {
		t.Fatal("oversized body executed tool")
	}
}
