package mcp //nolint:testpackage // Verify the real SDK output and schema.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSearchSDKPreservesPublicRetrievalReport(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	scope := memory.Scope{Key: "test:retrieval", Kind: memory.ScopeKindPersonal}
	server, err := NewServer(Config{Enabled: true, Searcher: recallSearcherFunc(
		func(context.Context, memory.RecallRequest) (memory.RecallResponse, error) {
			response := mismatchedSearchResponse(scope)
			response.Retrieval = &memory.RetrievalReport{Effective: "hybrid", Requested: "hybrid",
				ModelSpacePrefix: "abcdef012345", ScannedChunks: 10}

			return response, nil
		})})
	requireNoError(t, err, "server: %v")
	session := connectSearchWatermarkClient(t, ctx, server)
	handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil)

	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "report-test", Version: "1"}, nil)
	httpSession, err := client.Connect(ctx, &sdkmcp.StreamableClientTransport{Endpoint: httpServer.URL}, nil)
	requireNoError(t, err, "HTTP connect: %v")

	defer httpSession.Close()

	for _, session := range []*sdkmcp.ClientSession{session, httpSession} {
		result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: SearchToolName,
			Arguments: map[string]any{"scope_key": scope.Key, "scope_kind": scope.Kind, "query": "reference", "limit": 1}})
		requireNoError(t, err, "search: %v")
		body, err := json.Marshal(result.StructuredContent)
		requireNoError(t, err, "marshal: %v")

		var output map[string]json.RawMessage
		requireNoError(t, json.Unmarshal(body, &output), "decode: %v")

		if string(output["retrieval"]) != `{"effective":"hybrid"}` {
			t.Fatalf("retrieval missing or private fields exposed: %s", body)
		}
	}
}
