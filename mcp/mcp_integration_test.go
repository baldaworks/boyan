//go:build integration

package mcp //nolint:testpackage // Exercises forgetScope within the package.

import (
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestHandlerServesStreamableHTTPTools(t *testing.T) {
	t.Parallel()

	scope := boyan.Scope{Key: "telegram:http-mcp", Kind: boyan.ScopeKindPersonal}
	forgottenAt := time.Date(2026, time.August, 9, 13, 0, 0, 0, time.UTC)
	backend := &forgetterSpy{
		scopeOutcome: boyan.ForgetOutcome{
			SchemaVersion: boyan.DerivedSchemaVersionV1,
			OperationID:   "forget-op-http",
			Kind:          boyan.ForgetKindScope,
			Scope:         scope,
			ScopeVersion:  2,
		},
	}

	handler := httpToolsTestHandler(t, backend, forgottenAt)

	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)

	server := httptest.NewServer(mux)
	defer server.Close()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)

	conn, err := client.Connect(
		context.Background(),
		&sdkmcp.StreamableClientTransport{Endpoint: server.URL + "/mcp"},
		nil,
	)
	requireNoError(t, err, "Connect() error = %v")

	defer conn.Close()

	assertHTTPToolNames(t, conn)

	result, err := conn.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: SearchToolName,
		Arguments: map[string]any{
			"scope_key":  scope.Key,
			"scope_kind": scope.Kind,
			"query":      "mcp",
			"limit":      1,
		},
	})
	requireNoError(t, err, "CallTool(search) error = %v")

	output := decodeStructuredContent[SearchOutput](t, result)
	if !output.OK || output.Scope == nil || *output.Scope != scope ||
		len(output.Results) != 1 ||
		output.Results[0].Text != "mcp reference" {
		t.Fatalf("search output = %#v", output)
	}

	assertHTTPForgetScopeOutcome(t, conn, scope, backend, forgottenAt)
}

func assertHTTPForgetScopeOutcome(
	t *testing.T,
	conn *sdkmcp.ClientSession,
	scope boyan.Scope,
	backend *forgetterSpy,
	forgottenAt time.Time,
) {
	t.Helper()

	forgetResult, err := conn.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: ForgetScopeToolName,
		Arguments: map[string]any{
			"scope_key":         scope.Key,
			"scope_kind":        scope.Kind,
			"request_id":        "forget-request-1",
			"confirm_scope_key": scope.Key,
		},
	})
	requireNoError(t, err, "CallTool(forget_scope) error = %v")

	forgetOutput := decodeStructuredContent[ForgetOutput](t, forgetResult)
	if !forgetOutput.OK || forgetOutput.OperationID != "forget-op-http" ||
		forgetOutput.ForgottenAt == nil ||
		!forgetOutput.ForgottenAt.Equal(forgottenAt) {
		t.Fatalf("forget output = %#v", forgetOutput)
	}

	if backend.scopeCommand.Scope != scope ||
		backend.scopeCommand.RequestID != "forget-request-1" ||
		!backend.scopeCommand.ForgottenAt.Equal(forgottenAt) {
		t.Fatalf("scope command = %#v", backend.scopeCommand)
	}
}

func httpToolsTestHandler(t *testing.T, backend *forgetterSpy, forgottenAt time.Time) http.Handler {
	t.Helper()

	handler, err := NewHandler(Config{
		Enabled: true,
		Now:     func() time.Time { return forgottenAt },
		Searcher: recallSearcherFunc(func(_ context.Context, request boyan.RecallRequest) (boyan.RecallResponse, error) {
			return boyan.RecallResponse{
				SchemaVersion:  boyan.RecallSchemaVersionV1,
				Trust:          boyan.ReferenceTrustUntrusted,
				Scope:          request.Scope,
				ScopeChangeSeq: 1,
				Results: []boyan.RecallReference{{
					SchemaVersion:  boyan.RecallSchemaVersionV1,
					Trust:          boyan.ReferenceTrustUntrusted,
					Scope:          request.Scope,
					ItemID:         "item-1",
					RevisionID:     "revision-1",
					Revision:       1,
					Kind:           boyan.MemoryKindState,
					State:          boyan.RevisionStateActive,
					MemoryKey:      "memory-key-1",
					Text:           "mcp reference",
					CreatedAt:      time.Unix(1, 0).UTC(),
					ScopeChangeSeq: 1,
				}},
			}, nil
		}),
		Tracer: traceReaderFunc(func(_ context.Context, request boyan.TraceRequest) (boyan.TraceResponse, error) {
			return traceResponse(request.Scope, request.Root), nil
		}),
		Forgetter: backend,
	})
	requireNoError(t, err, "NewHandler() error = %v")

	return handler
}

func assertHTTPToolNames(t *testing.T, conn *sdkmcp.ClientSession) {
	t.Helper()

	toolsResult, err := conn.ListTools(context.Background(), nil)
	requireNoError(t, err, "ListTools() error = %v")

	gotTools := map[string]bool{}
	for _, tool := range toolsResult.Tools {
		gotTools[tool.Name] = true
	}

	wantTools := map[string]bool{
		SearchToolName:       true,
		TraceToolName:        true,
		ForgetSourceToolName: true,
		ForgetScopeToolName:  true,
	}
	if !maps.Equal(gotTools, wantTools) {
		t.Fatalf("tool names = %#v, want %#v", gotTools, wantTools)
	}
}
