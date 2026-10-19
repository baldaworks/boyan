package boyan_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	index "github.com/baldaworks/boyan/index/sqlite"
	core "github.com/baldaworks/boyan/internal/canonicalcore"
	projection "github.com/baldaworks/boyan/internal/projection"
	transport "github.com/baldaworks/boyan/mcp"
	hostapi "github.com/baldaworks/boyan/pkg/boyan"
	m "github.com/baldaworks/boyan/pkg/memory"
	b "github.com/baldaworks/boyan/store/sqlite"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type integrationExtractor struct{}

func (integrationExtractor) ExtractCanonicalSemantics(_ context.Context,
	request m.CanonicalExtractionRequest) ([]m.SemanticCandidate,
	error) {
	message := request.Turn.Messages[0]

	evidence,
		err := m.NewEvidenceRef(request.Turn.SourceID,
		message.MessageID,
		message.Role,
		message.Text,
		0,
		uint32(len(message.Text)),
		m.AssertionModeUser)
	if err != nil {
		return nil, err
	}

	return []m.SemanticCandidate{{Kind: m.MemoryKindState,
			Subject:   "owner",
			Predicate: "lives in",
			Memory: m.MemoryCandidate{Kind: m.MemoryKindState,
				Statement:   message.Text,
				Temporal:    m.Temporal{ObservedAt: request.Turn.CompletedAt},
				Evidence:    []m.EvidenceRef{evidence},
				Sensitivity: m.SensitivityStandard,
				Retention:   m.RetentionClassStandard}}},
		nil
}

func TestPublicHostAndMCPNativeMemoryLifecycle(t *testing.T) {
	t.Parallel()
	config := testHostConfig(t)
	scope := m.Scope{Key: "test:lifecycle", Kind: m.ScopeKindPersonal}
	conversation := m.ConversationRef{ConversationID: "conversation", AgentConversationID: "agent"}
	first,
		err := m.NewTurn(scope,
		conversation,
		"turn-first",
		time.Date(2026,
			time.October,
			11,
			0,
			0,
			0,
			0,
			time.UTC),
		"Я живу в Бишкеке",
		"OK")
	requireNoError(t, err, "first: %v")
	initial := persistIntegrationTurn(t, config, first)
	host, client := startIntegrationHost(t, config)
	initialReference := assertHostAndMCPRecall(t, host, client, first, initial.ChangeSeq, nil, 1)
	assertHostAndMCPTrace(t, host, client, scope, initialReference, 1)
	requireNoError(t, client.Close(), "client close: %v")
	requireNoError(t, host.Close(), "host close for durable update: %v")

	second, err := m.NewTurn(scope, conversation, "turn-second", first.CompletedAt.Add(2*time.Hour), "Я живу в Оше", "OK")
	requireNoError(t, err, "second: %v")
	updated := persistIntegrationTurn(t, config, second)
	host, client = startIntegrationHost(t, config)

	current := assertHostAndMCPRecall(t, host, client, second, updated.ChangeSeq, nil, 1)
	if current.ItemID != initialReference.ItemID || current.Revision != 2 {
		t.Fatalf("identity/history changed: %+v -> %+v",
			initialReference,
			current)
	}

	assertHostAndMCPTrace(t, host, client, scope, current, 2)

	between := first.CompletedAt.Add(time.Hour)
	assertHostAndMCPRecall(t, host, client, second, updated.ChangeSeq, &between, 0)
	assertHostAndMCPForget(t, host, client, second, current)
}

func persistIntegrationTurn(t *testing.T, config hostapi.Config, turn m.Turn) m.CanonicalMutationOutcome {
	t.Helper()

	store, err := b.Open(t.Context(), b.Config{Path: filepath.Join(config.StateDir, "canonical", "canonical.sqlite")})

	requireNoError(t, err, "store: %v")
	defer func() { requireNoError(t, store.Close(), "store close: %v") }()

	requireNoError(t, store.PrepareSourcePrivacy(t.Context()), "prepare fixture: %v")

	processor, err := core.NewCanonicalTurnProcessor(store, integrationExtractor{}, m.PolicyRegistry{Version: "policy-v1"})
	requireNoError(t, err, "processor: %v")
	outcome, err := processor.ProcessTurn(context.Background(), turn, m.LegacyDerivationRef())
	requireNoError(t, err, "deterministic native ingest: %v")

	reader := b.NewCanonicalReader(store)
	indexer, err := index.Open(t.Context(), config.StateDir, index.Options{})

	requireNoError(t, err, "index: %v")
	defer func() { requireNoError(t, indexer.Close(), "index close: %v") }()

	applier, err := index.NewSQLiteCanonicalApplier(indexer, reader, store)

	requireNoError(t, err, "applier: %v")
	runtime, err := projection.NewProjectionRuntime(store, store, applier, "sqlite", 128)

	requireNoError(t, err, "projection runtime: %v")
	defer func() { requireNoError(t, runtime.Close(t.Context()), "projection close: %v") }()

	requireNoError(t, runtime.Sync(context.Background(), turn.Scope), "synchronize durable checkpoint: %v")

	return outcome
}

func startIntegrationHost(t *testing.T, config hostapi.Config) (*hostapi.Host, *sdk.ClientSession) {
	t.Helper()

	host, err := hostapi.New(context.Background(), config)
	requireNoError(t, err, "public host: %v")
	t.Cleanup(func() { _ = host.Close() })
	requireNoError(t, host.Start(context.Background()), "start: %v")

	client := sdk.NewClient(&sdk.Implementation{Name: "native-flow-test", Version: "1"}, nil)
	connection,
		err := client.Connect(context.Background(),
		&sdk.StreamableClientTransport{Endpoint: "http://" + host.Addr() + "/mcp"},
		nil)
	requireNoError(t, err, "MCP connect: %v")
	t.Cleanup(func() { _ = connection.Close() })

	return host, connection
}

func assertHostAndMCPRecall(t *testing.T,
	host *hostapi.Host,
	client *sdk.ClientSession,
	turn m.Turn,
	minimum uint64,
	asOf *time.Time,
	count int) m.RecallReference {
	t.Helper()

	request := m.RecallRequest{SchemaVersion: m.RecallSchemaVersionV1,
		Scope:             turn.Scope,
		Query:             "живу",
		Limit:             10,
		AsOf:              asOf,
		MinScopeChangeSeq: minimum}
	response, err := host.Search(context.Background(), request)
	requireNoError(t, err, "Host.Search: %v")
	requireNoError(t, response.Validate(request), "recall validation: %v")

	if len(response.Results) != count {
		t.Fatalf("host recall: %+v", response)
	}

	args := map[string]any{"scope_key": turn.Scope.Key,
		"scope_kind":           turn.Scope.Kind,
		"query":                "живу",
		"limit":                10,
		"min_scope_change_seq": minimum,
		"memory_kind":          "state"}
	if asOf != nil {
		args["as_of"] = asOf.Format(time.RFC3339Nano)
	}

	result, err := client.CallTool(
		context.Background(),
		&sdk.CallToolParams{Name: transport.SearchToolName, Arguments: args},
	)
	requireNoError(t, err, "MCP search: %v")

	output := decodeNativeTool[transport.SearchOutput](t, result)
	if !output.OK || len(output.Results) != count {
		t.Fatalf("MCP recall: %+v", output)
	}

	if count == 0 {
		return m.RecallReference{}
	}

	if response.Results[0].Text != turn.Messages[0].Text ||
		output.Results[0].RevisionID != response.Results[0].RevisionID {
		t.Fatalf("Host/MCP current memory mismatch: %+v %+v",
			response,
			output)
	}

	return response.Results[0]
}

func assertHostAndMCPTrace(t *testing.T,
	host *hostapi.Host,
	client *sdk.ClientSession,
	scope m.Scope,
	root m.RecallReference,
	count int) {
	t.Helper()

	request := m.TraceRequest{
		Scope:    scope,
		Root:     m.RevisionRef{ItemID: root.ItemID, RevisionID: root.RevisionID},
		MaxNodes: 10,
	}
	response, err := host.Trace(context.Background(), request)
	requireNoError(t, err, "Host.Trace: %v")
	requireNoError(t, response.Validate(10), "trace validation: %v")

	result,
		err := client.CallTool(context.Background(),
		&sdk.CallToolParams{Name: transport.TraceToolName,
			Arguments: map[string]any{"scope_key": scope.Key,
				"scope_kind":  scope.Kind,
				"item_id":     root.ItemID,
				"revision_id": root.RevisionID,
				"max_nodes":   10}})
	requireNoError(t, err, "MCP trace: %v")

	output := decodeNativeTool[transport.TraceOutput](t, result)
	if !output.OK || len(output.Revisions) != count || len(output.Sources) != count {
		t.Fatalf("MCP trace graph: %+v", output)
	}

	graph := m.TraceResponse{SchemaVersion: m.DerivedSchemaVersionV1,
		Trust:     m.ReferenceTrustUntrusted,
		Scope:     *output.Scope,
		Root:      *output.Root,
		Revisions: output.Revisions,
		Sources:   output.Sources}
	requireNoError(t, graph.Validate(10), "MCP trace validation: %v")

	if graph.Revisions[0].Canonical == nil ||
		count == 2 && graph.Revisions[0].Canonical.Supersedes == nil {
		t.Fatal("native identity/supersession missing")
	}
}

func decodeNativeTool[T any](t *testing.T, result *sdk.CallToolResult) T {
	t.Helper()

	content, err := json.Marshal(result.StructuredContent)
	requireNoError(t, err, "structured content: %v")

	var output T
	requireNoError(t, json.Unmarshal(content, &output), "decode: %v")

	return output
}

func assertHostAndMCPForget(t *testing.T,
	host *hostapi.Host,
	client *sdk.ClientSession,
	turn m.Turn,
	root m.RecallReference) {
	t.Helper()

	command := m.ForgetSourceCommand{SchemaVersion: m.DerivedSchemaVersionV1,
		Source: m.SourceRef{Scope: turn.Scope,
			ExportID:       turn.ExportID,
			ConversationID: turn.Conversation.ConversationID,
			SourceTurnID:   turn.SourceTurnID},
		ForgottenAt: time.Now().UTC()}
	_, err := host.ForgetSource(context.Background(), command)
	requireNoError(t, err, "Host.ForgetSource: %v")

	args := map[string]any{"scope_key": turn.Scope.Key,
		"scope_kind":      turn.Scope.Kind,
		"export_id":       turn.ExportID,
		"conversation_id": turn.Conversation.ConversationID,
		"source_turn_id":  turn.SourceTurnID}
	result,
		err := client.CallTool(context.Background(),
		&sdk.CallToolParams{Name: transport.ForgetSourceToolName,
			Arguments: args})
	requireNoError(t, err, "MCP repeat forget: %v")

	output := decodeNativeTool[transport.ForgetOutput](t, result)
	if !output.OK {
		t.Fatalf("repeat forget failed: %+v", output)
	}

	assertHostAndMCPRecall(t, host, client, turn, 0, nil, 0)

	_,
		err = host.Trace(context.Background(),
		m.TraceRequest{Scope: turn.Scope,
			Root: m.RevisionRef{ItemID: root.ItemID,
				RevisionID: root.RevisionID},
			MaxNodes: 10})
	if err == nil {
		t.Fatal("forgotten native trace exposed")
	}

	result,
		err = client.CallTool(context.Background(),
		&sdk.CallToolParams{Name: transport.TraceToolName,
			Arguments: map[string]any{"scope_key": turn.Scope.Key,
				"scope_kind":  turn.Scope.Kind,
				"item_id":     root.ItemID,
				"revision_id": root.RevisionID}})
	requireNoError(t, err, "MCP forgotten trace: %v")

	failed := decodeNativeTool[transport.TraceOutput](t, result)
	if failed.OK || len(failed.Revisions) != 0 || len(failed.Sources) != 0 {
		t.Fatalf("forgotten graph disclosed: %+v", failed)
	}
}
