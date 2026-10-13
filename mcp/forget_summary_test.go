package mcp //nolint:testpackage // Exercises forget output mapping and inferred SDK schema without network sockets.

import (
	"context"
	"encoding/json"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestForgetSummaryIsSerializedAndAdvertisedByMCP(t *testing.T) {
	scope := memory.Scope{Key: "test:summary", Kind: memory.ScopeKindPersonal}
	outcome := memory.ForgetOutcome{
		SchemaVersion: memory.DerivedSchemaVersionV1, OperationID: "forget", Kind: memory.ForgetKindScope,
		Scope: scope, ScopeVersion: 1, SourceCount: 1025, RevisionCount: 1025, ReferencesTruncated: true,
	}
	server, err := NewServer(Config{Enabled: true, Forgetter: &forgetterSpy{scopeOutcome: outcome}})
	requireNoError(t, err, "server: %v")

	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	requireNoError(t, err, "server connect: %v")
	t.Cleanup(func() { _ = serverSession.Close() })

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "summary-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	requireNoError(t, err, "client connect: %v")
	t.Cleanup(func() { _ = session.Close() })

	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: ForgetScopeToolName,
		Arguments: map[string]any{
			"scope_key": scope.Key, "scope_kind": string(scope.Kind), "request_id": "request", "confirm_scope_key": scope.Key,
		}})
	requireNoError(t, err, "call forget: %v")

	output := decodeStructuredContent[map[string]any](t, result)
	if output["source_count"] != float64(1025) || output["revision_count"] != float64(1025) ||
		output["references_truncated"] != true {
		t.Fatalf("summary not serialized: %+v", output)
	}

	if _, ok := output["sources"]; ok {
		t.Fatalf("summary presents source array: %+v", output)
	}

	if _, ok := output["revisions"]; ok {
		t.Fatalf("summary presents revision array: %+v", output)
	}

	tools, err := session.ListTools(context.Background(), nil)
	requireNoError(t, err, "list tools: %v")

	for _, tool := range tools.Tools {
		if tool.Name != ForgetScopeToolName {
			continue
		}

		requireForgetSummarySchema(t, tool.OutputSchema)

		return
	}

	t.Fatal("forget tool missing")
}

func requireForgetSummarySchema(t *testing.T, schema any) {
	t.Helper()

	encoded, err := json.Marshal(schema)
	requireNoError(t, err, "schema marshal: %v")

	var decoded struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	requireNoError(t, json.Unmarshal(encoded, &decoded), "schema decode: %v")

	for _, name := range []string{"source_count", "revision_count", "references_truncated"} {
		if len(decoded.Properties[name]) == 0 {
			t.Fatalf("forget schema missing %s: %s", name, encoded)
		}
	}
}
