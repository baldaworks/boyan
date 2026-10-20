//go:build integration

package standaloneruntime //nolint:testpackage // Exercises status within the package.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	boyanmcp "github.com/baldaworks/boyan/mcp"
	"github.com/rs/zerolog"
)

func TestLoggingMCPStream(t *testing.T) {
	handler, err := boyanmcp.NewHandler(boyanmcp.Config{Enabled: true})
	requireNoError(t, err, "%v")

	server := httptest.NewServer(
		httpBodyLimits(
			standaloneHTTPConfig{RequestTimeout: 30 * time.Millisecond, MaxBodyBytes: 1024},
			httpLoggingMiddleware(zerolog.Nop(), handler),
		),
	)
	defer server.Close()

	client := &http.Client{Timeout: time.Second}

	initialized := initializeLoggingMCPClient(t, client, server.URL)
	defer initialized.Body.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	requireNoError(t, err, "%v")

	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Mcp-Session-Id", initialized.Header.Get("Mcp-Session-Id"))

	stream, err := client.Do(request)
	requireNoError(t, err, "%v")

	defer stream.Body.Close()

	if stream.StatusCode != http.StatusOK {
		t.Fatalf("stream status: %d", stream.StatusCode)
	}

	streamEnded := make(chan error, 1)

	go func() { _, err := io.Copy(io.Discard, stream.Body); streamEnded <- err }()

	select {
	case err := <-streamEnded:
		t.Fatalf("GET stream ended during upload timeout window: %v", err)
	case <-time.After(120 * time.Millisecond):
	}
}

func initializeLoggingMCPClient(t *testing.T, client *http.Client, endpoint string) *http.Response {
	t.Helper()

	request, err := http.NewRequest(
		http.MethodPost,
		endpoint,
		strings.NewReader(
			("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"pro" +
				"tocolVersion\":\"2025-03-26\",\"capabilities\":{},\"clientInfo\":{\"" +
				"name\":\"test\",\"version\":\"1\"}}}"),
		),
	)
	requireNoError(t, err, "%v")

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")

	initialized, err := client.Do(request)
	requireNoError(t, err, "%v")

	if _, err := io.Copy(io.Discard, initialized.Body); err != nil {
		t.Fatal(err)
	}

	if initialized.StatusCode != http.StatusOK {
		t.Fatalf("initialize status: %d", initialized.StatusCode)
	}

	return initialized
}
