package canonicalcore_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	core "github.com/baldaworks/boyan/internal/canonicalcore"
	boyanmcp "github.com/baldaworks/boyan/mcp"
	m "github.com/baldaworks/boyan/pkg/memory"
	b "github.com/baldaworks/boyan/store/badger"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const mixedTraceSafe = "Я живу в Бишкеке"
const mixedTraceSecret = "token-PRIVACY-private"

func TestSensitiveSiblingTraceDeniesWholeGraphAndMCP(t *testing.T) {
	for _, separate := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-message", true: "separate-message"}[separate], func(t *testing.T) {
			dir := t.TempDir()
			store, err := b.OpenCanonicalStore(dir)
			requireNoError(t, err, "open: %v")
			t.Cleanup(func() { _ = store.Close() })

			reader, err := b.NewCanonicalReader(store)
			requireNoError(t, err, "reader: %v")

			extractor := canonicalExtractorFunc(mixedTraceCandidates)
			processor, err := core.NewCanonicalTurnProcessor(store, extractor, m.PolicyRegistry{Version: "policy-v1"})
			requireNoError(t, err, "processor: %v")
			turn := mixedTraceTurn(t, separate)
			outcome, err := processor.ProcessTurn(context.Background(), turn, testProcessorDerivation())
			requireNoError(t, err, "process: %v")
			records, err := reader.LoadRecallRecords(context.Background(), turn.Scope, outcome.RevisionIDs)
			requireNoError(t, err, "recall: %v")

			if len(records) != 1 || records[0].Text != mixedTraceSafe {
				t.Fatalf("benign recall changed: %+v", records)
			}

			revisions, err := store.LoadCanonicalRevisions(context.Background(), m.CanonicalRevisionReadRequest{
				Scope: turn.Scope, RevisionIDs: outcome.RevisionIDs,
			})
			requireNoError(t, err, "revisions: %v")

			for phase := 0; phase < 2; phase++ {
				for _, revision := range revisions {
					assertMixedRevisionDenied(t, reader, turn.Scope, revision)
				}

				if phase == 0 {
					requireNoError(t, store.Close(), "close before restart: %v")
					store, err = b.OpenCanonicalStore(dir)
					requireNoError(t, err, "reopen: %v")
					reader, err = b.NewCanonicalReader(store)
					requireNoError(t, err, "reader after restart: %v")
				}
			}
		})
	}
}

func mixedTraceTurn(t *testing.T, separate bool) m.Turn {
	t.Helper()

	scope := m.Scope{Key: "test:mixed", Kind: m.ScopeKindPersonal}
	conversation := m.ConversationRef{ConversationID: "c", AgentConversationID: "a"}
	text := mixedTraceSafe + ". " + mixedTraceSecret

	var tools []m.Message

	if separate {
		text = mixedTraceSafe
		tools = []m.Message{{ToolName: "private-config", ToolCallID: "call-1", Text: mixedTraceSecret}}
	}

	turn, err := m.NewTerminalTurnWithTools(scope, conversation, "mixed",
		time.Now().UTC(), text, "OK", tools, m.TurnTerminalStatusSuccess)
	requireNoError(t, err, "turn: %v")

	return turn
}

func mixedTraceCandidates(ctx context.Context, request m.CanonicalExtractionRequest) ([]m.SemanticCandidate, error) {
	candidates, err := extractTestStateCandidate(ctx, request)
	if err != nil {
		return nil, err
	}

	safe := candidates[0]
	safe.Memory.Statement = mixedTraceSafe
	safe.Predicate = "residence"
	message := request.Turn.Messages[0]

	ref, err := m.NewEvidenceRef(request.Turn.SourceID, message.MessageID,
		message.Role, message.Text, 0, uint32(len(mixedTraceSafe)), m.AssertionModeUser)
	if err != nil {
		return nil, err
	}

	safe.Memory.Evidence = []m.EvidenceRef{ref}
	sensitive := safe
	sensitive.Predicate = "api_token"
	sensitive.Memory.Statement = mixedTraceSecret
	sensitive.Memory.Sensitivity = m.SensitivitySensitive
	mode := m.AssertionModeUser

	if len(request.Turn.Messages) > 2 {
		message = request.Turn.Messages[2]
		mode = m.AssertionModeTrustedTool
	}

	start := uint32(strings.Index(message.Text, mixedTraceSecret))

	ref, err = m.NewEvidenceRef(request.Turn.SourceID, message.MessageID,
		message.Role, message.Text, start, start+uint32(len(mixedTraceSecret)), mode)
	if err != nil {
		return nil, err
	}

	sensitive.Memory.Evidence = []m.EvidenceRef{ref}

	return []m.SemanticCandidate{safe, sensitive}, nil
}

func assertMixedTraceDenied(t *testing.T, response m.TraceResponse, err error) {
	t.Helper()

	code, _, ok := m.ClassifyError(err)
	if !ok || code != m.CodeForgotten || len(response.Revisions) != 0 || len(response.Sources) != 0 {
		t.Fatalf("trace not denied: %+v %v", response, err)
	}

	encoded, _ := json.Marshal(response)
	if strings.Contains(string(encoded), mixedTraceSecret) || strings.Contains(err.Error(), mixedTraceSecret) {
		t.Fatal("protected sentinel escaped")
	}
}

func assertMixedMCPDenied(t *testing.T, tracer boyanmcp.Tracer, request m.TraceRequest) {
	t.Helper()

	server, err := boyanmcp.NewServer(boyanmcp.Config{Enabled: true, Tracer: tracer})
	requireNoError(t, err, "MCP server: %v")

	st, ct := sdkmcp.NewInMemoryTransports()
	ss, err := server.Connect(context.Background(), st, nil)
	requireNoError(t, err, "server connect: %v")

	defer ss.Close()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "privacy-test", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), ct, nil)
	requireNoError(t, err, "client connect: %v")

	defer cs.Close()

	result, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: boyanmcp.TraceToolName, Arguments: map[string]any{
			"scope_key": request.Scope.Key, "scope_kind": string(request.Scope.Kind),
			"item_id": request.Root.ItemID, "revision_id": request.Root.RevisionID, "max_nodes": 10,
		},
	})
	requireNoError(t, err, "MCP trace: %v")
	encoded, err := json.Marshal(result)
	requireNoError(t, err, "MCP serialize: %v")

	if !result.IsError || strings.Contains(string(encoded), mixedTraceSecret) ||
		!strings.Contains(string(encoded), string(m.CodeForgotten)) {
		t.Fatalf("MCP denial invalid: %s", encoded)
	}
}

func assertMixedRevisionDenied(t *testing.T, reader *b.CanonicalReader, scope m.Scope, revision m.MemoryRevision) {
	t.Helper()

	request := m.TraceRequest{Scope: scope,
		Root: m.RevisionRef{ItemID: revision.ItemID, RevisionID: revision.RevisionID}, MaxNodes: 10,
	}
	response, err := reader.Trace(context.Background(), request)
	assertMixedTraceDenied(t, response, err)
	assertMixedMCPDenied(t, reader, request)
}
