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

func TestSearchRejectsFilterMismatchedReferenceThroughSDK(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	scope := memory.Scope{Key: "test:response-validation", Kind: memory.ScopeKindPersonal}
	server, err := NewServer(Config{Enabled: true, Searcher: recallSearcherFunc(
		func(context.Context, memory.RecallRequest) (memory.RecallResponse, error) {
			return mismatchedSearchResponse(scope), nil
		})})
	requireNoError(t, err, "server: %v")

	session := connectSearchWatermarkClient(t, ctx, server)
	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: SearchToolName,
		Arguments: map[string]any{
			"scope_key": scope.Key, "scope_kind": scope.Kind, "query": "reference",
			"limit": 1, "memory_kind": memory.MemoryKindEvent,
		},
	})
	requireNoError(t, err, "call search: %v")

	output := decodeStructuredContent[SearchOutput](t, result)
	if !result.IsError || output.OK || output.Error == nil || len(output.Results) != 0 || output.Scope != nil {
		t.Fatalf("mismatched search was acknowledged: result=%+v output=%+v", result, output)
	}

	encoded, err := json.Marshal(result)
	requireNoError(t, err, "encode result: %v")

	if strings.Contains(string(encoded), "private mismatched reference") || strings.Contains(string(encoded), scope.Key) {
		t.Fatalf("failure leaked rejected response: %s", encoded)
	}
}

func mismatchedSearchResponse(scope memory.Scope) memory.RecallResponse {
	category := memory.AtomCategoryFact

	return memory.RecallResponse{
		SchemaVersion: memory.RecallSchemaVersionV1, Trust: memory.ReferenceTrustUntrusted,
		Scope: scope, ScopeChangeSeq: 7,
		Results: []memory.RecallReference{{
			SchemaVersion: memory.RecallSchemaVersionV1, Trust: memory.ReferenceTrustUntrusted,
			Scope: scope, ItemID: "item-1", RevisionID: "revision-1", Revision: 1,
			Kind: memory.MemoryKindState, State: memory.RevisionStateActive, Category: &category,
			MemoryKey: "memory-key-1", Text: "private mismatched reference",
			CreatedAt: time.Date(2026, time.October, 14, 12, 0, 0, 0, time.UTC),
			Score:     2.5, Explain: memory.RecallScore{Lexical: 1, Freshness: 0.5, ExactScope: 1, Total: 2.5},
			Evidence: []memory.EvidenceRef{{
				SourceID: "source-1", MessageID: "message-1", Role: memory.MessageRoleUser,
				EndByte: 4, AssertionMode: memory.AssertionModeUser,
			}},
			ScopeChangeSeq: 7,
		}},
	}
}
