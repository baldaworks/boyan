package mcp //nolint:testpackage // Exercises the registered search tool through the real SDK transport.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSearchMinimumWatermarkThroughSDK(t *testing.T) {
	t.Parallel()

	for _, test := range searchWatermarkCases() {
		for _, empty := range []bool{true, false} {
			name := test.name + "/reference"
			if empty {
				name = test.name + "/empty"
			}

			t.Run(name, func(t *testing.T) {
				t.Parallel()

				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()

				scope := memory.Scope{Key: "test:watermark", Kind: memory.ScopeKindPersonal}

				response := searchWatermarkResponse(t, scope, test.counter, empty)

				server, err := NewServer(Config{Enabled: true, Searcher: recallSearcherFunc(
					func(_ context.Context, request memory.RecallRequest) (memory.RecallResponse, error) {
						if request.MinScopeChangeSeq != test.minimum || request.Scope != scope {
							t.Errorf("search request = %+v", request)
						}

						return response, nil
					})})
				requireNoError(t, err, "server: %v")

				session := connectSearchWatermarkClient(t, ctx, server)

				result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
					Name: SearchToolName,
					Arguments: map[string]any{
						"scope_key": scope.Key, "scope_kind": scope.Kind, "query": "reference",
						"limit": 1, "min_scope_change_seq": test.minimum,
					},
				})
				requireNoError(t, err, "call search: %v")

				output := decodeStructuredContent[SearchOutput](t, result)
				if test.reject {
					assertSearchWatermarkConflict(t, result, output, scope)

					return
				}

				if !empty && response.ScopeChangeSeq == 0 {
					assertSearchWatermarkRejected(t, result, output, scope, memory.CodeInvalidDerived)

					return
				}

				assertSearchWatermarkSuccess(t, result, output, scope, test.wantSeq, len(response.Results))
			})
		}
	}
}

func assertSearchWatermarkConflict(
	t *testing.T, result *sdkmcp.CallToolResult, output SearchOutput, scope memory.Scope,
) {
	assertSearchWatermarkRejected(t, result, output, scope, memory.CodeConflict)
}

func assertSearchWatermarkRejected(
	t *testing.T,
	result *sdkmcp.CallToolResult,
	output SearchOutput,
	scope memory.Scope,
	wantCode memory.ErrorCode,
) {
	t.Helper()

	if !result.IsError || output.OK || output.Error == nil || output.Error.Code != string(wantCode) ||
		output.Scope != nil || output.ScopeChangeSeq != 0 || len(output.Results) != 0 ||
		output.DataClassification != DataClassificationNone {
		t.Fatalf("expected safe %s, got %+v, error=%v", wantCode, output, result.IsError)
	}

	assertSearchWatermarkRedacted(t, result, scope)
}

func assertSearchWatermarkSuccess(
	t *testing.T, result *sdkmcp.CallToolResult, output SearchOutput, scope memory.Scope, sequence uint64, count int,
) {
	t.Helper()

	if result.IsError || !output.OK || output.ScopeChangeSeq != sequence ||
		output.Scope == nil || *output.Scope != scope || len(output.Results) != count {
		t.Fatalf("search success = %+v, error=%v", output, result.IsError)
	}
}

func assertSearchWatermarkRedacted(t *testing.T, result *sdkmcp.CallToolResult, scope memory.Scope) {
	t.Helper()

	encoded, err := json.Marshal(result)
	requireNoError(t, err, "encode result: %v")

	if strings.Contains(string(encoded), "private reference") || strings.Contains(string(encoded), scope.Key) {
		t.Fatalf("failure leaked response content: %s", encoded)
	}
}

func connectSearchWatermarkClient(t *testing.T, ctx context.Context, server *sdkmcp.Server) *sdkmcp.ClientSession {
	t.Helper()

	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	requireNoError(t, err, "server connect: %v")
	t.Cleanup(func() { _ = serverSession.Close() })

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "watermark-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	requireNoError(t, err, "client connect: %v")
	t.Cleanup(func() { _ = session.Close() })

	return session
}

func searchWatermarkResponse(t *testing.T, scope memory.Scope, counter string, empty bool) memory.RecallResponse {
	t.Helper()

	var response memory.RecallResponse
	requireNoError(t, json.Unmarshal([]byte(`{"schema_version":"v1"`+counter+`}`), &response), "decode: %v")
	response.SchemaVersion = memory.RecallSchemaVersionV1
	response.Trust = memory.ReferenceTrustUntrusted
	response.Scope = scope

	if !empty {
		response.Results = []memory.RecallReference{{
			SchemaVersion: memory.RecallSchemaVersionV1, Trust: memory.ReferenceTrustUntrusted,
			Scope: scope, State: memory.RevisionStateActive, Text: "private reference",
			ItemID: "item", RevisionID: "revision", Revision: 1, Kind: memory.MemoryKindState,
			MemoryKey: "memory-key", CreatedAt: time.Unix(1, 0).UTC(),
			ScopeChangeSeq: response.ScopeChangeSeq,
		}}
	}

	return response
}

type searchWatermarkCase struct {
	name    string
	counter string
	minimum uint64
	wantSeq uint64
	reject  bool
}

func searchWatermarkCases() []searchWatermarkCase {
	return []searchWatermarkCase{
		{name: "missing", minimum: 42, reject: true},
		{name: "zero", counter: `,"scope_change_seq":0`, minimum: 42, reject: true},
		{name: "below", counter: `,"scope_change_seq":1`, minimum: 42, reject: true},
		{name: "matching", counter: `,"scope_change_seq":42`, minimum: 42, wantSeq: 42},
		{name: "exceeding", counter: `,"scope_change_seq":43`, minimum: 42, wantSeq: 43},
		{name: "zero_minimum_missing"},
		{name: "zero_minimum_zero", counter: `,"scope_change_seq":0`},
	}
}
