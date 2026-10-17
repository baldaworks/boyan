package boyan_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalcore"
	hostapi "github.com/baldaworks/boyan/pkg/boyan"
	"github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/badger"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type hostDenialExtractor struct{}

func (hostDenialExtractor) ExtractCanonicalSemantics(
	_ context.Context, req memory.CanonicalExtractionRequest,
) ([]memory.SemanticCandidate, error) {
	message := req.Turn.Messages[0]

	evidence, err := memory.NewEvidenceRef(req.Turn.SourceID, message.MessageID, message.Role, message.Text,
		0, uint32(len(message.Text)), memory.AssertionModeUser)
	if err != nil {
		return nil, err
	}

	return []memory.SemanticCandidate{{Kind: memory.MemoryKindState, Subject: "host-denial", Predicate: "home",
		Memory: memory.MemoryCandidate{Kind: memory.MemoryKindState, Statement: message.Text,
			Temporal: memory.Temporal{ObservedAt: req.Turn.CompletedAt}, Evidence: []memory.EvidenceRef{evidence},
			Sensitivity: memory.SensitivityStandard, Retention: memory.RetentionClassStandard}}}, nil
}

//nolint:cyclop,funlen // Real Host and HTTP MCP calls share one failed canonical outcome and exact replay.
func TestHostDenialCompletionErrors(t *testing.T) {
	cfg := testHostConfig(t)
	scope := memory.Scope{Kind: memory.ScopeKindPersonal, Key: "denial:host"}
	store, err := badger.OpenCanonicalStore(filepath.Join(cfg.StateDir, "badger"))
	requireNoError(t, err, "seed store: %v")
	turn, err := memory.NewTurn(scope,
		memory.ConversationRef{ConversationID: "host-denial", AgentConversationID: "agent"},
		"first", time.Now().UTC().Add(-time.Hour), "Bishkek home", "OK")
	requireNoError(t, err, "turn: %v")
	processor, err := canonicalcore.NewCanonicalTurnProcessor(store, hostDenialExtractor{},
		memory.PolicyRegistry{Version: "denial-policy"})
	requireNoError(t, err, "processor: %v")
	_, err = processor.ProcessTurn(t.Context(), turn, memory.LegacyDerivationRef())
	requireNoError(t, err, "canonical turn: %v")
	requireNoError(t, store.Close(), "close seed store: %v")
	host, err := hostapi.New(t.Context(), cfg)
	requireNoError(t, err, "host: %v")
	t.Cleanup(func() { _ = host.Close() })
	requireNoError(t, host.Start(t.Context()), "host start: %v")

	db, err := sql.Open("sqlite", filepath.Join(cfg.StateDir, "search", "search.sqlite"))
	requireNoError(t, err, "SQL inspection: %v")
	t.Cleanup(func() { _ = db.Close() })

	var count int
	requireNoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM lexical_record").Scan(&count),
		"positive public fixture: %v")

	if count != 1 {
		t.Fatalf("missing real projection: %d", count)
	}

	_, err = db.ExecContext(t.Context(), `CREATE TRIGGER host_denial_failure BEFORE DELETE ON lexical_record
BEGIN SELECT RAISE(ABORT,'private host denial fault'); END`)
	requireNoError(t, err, "inject required cleanup failure: %v")

	command := memory.ForgetScopeCommand{SchemaVersion: memory.DerivedSchemaVersionV1,
		Scope: scope, RequestID: "host-denial", ForgottenAt: time.Now().UTC()}
	outcome, err := host.ForgetScope(t.Context(), command)

	code, _, _ := memory.ClassifyError(err)
	if code != memory.CodeStoreFailure || outcome.OperationID == "" || outcome.RevisionCount != 1 ||
		strings.Contains(err.Error(), "private host") {
		t.Fatalf("Host lost completed outcome/classified error: %+v %v", outcome, err)
	}
	// The real Host handler exposes forgetting through Streamable HTTP MCP.
	// Transport errors retain the existing content-free shape, never a partial
	// successful outcome, even though Host's Go result preserves the receipt.
	server := httptest.NewServer(host.Handler())
	t.Cleanup(server.Close)

	client := sdk.NewClient(&sdk.Implementation{Name: "host-denial-test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &sdk.StreamableClientTransport{Endpoint: server.URL + "/mcp"}, nil)
	requireNoError(t, err, "HTTP MCP connect: %v")
	t.Cleanup(func() { _ = session.Close() })

	params := &sdk.CallToolParams{Name: "boyan.forget_scope", Arguments: map[string]any{
		"scope_key": scope.Key, "scope_kind": string(scope.Kind),
		"request_id": command.RequestID, "confirm_scope_key": scope.Key,
	}}
	result, err := session.CallTool(t.Context(), params)
	requireNoError(t, err, "HTTP MCP classified failure: %v")
	encoded, err := json.Marshal(result)
	requireNoError(t, err, "error envelope: %v")

	if !result.IsError || strings.Contains(string(encoded), "private host") ||
		strings.Contains(string(encoded), "scope_version") || strings.Contains(string(encoded), "partial") {
		t.Fatalf("changed public failure shape: %s", encoded)
	}

	_, err = db.ExecContext(t.Context(), "DROP TRIGGER host_denial_failure")
	requireNoError(t, err, "remove controlled fault: %v")
	replay, err := host.ForgetScope(t.Context(), command)
	requireNoError(t, err, "Host retry: %v")

	if !reflect.DeepEqual(replay, outcome) {
		t.Fatal("Host retry changed original receipt")
	}

	requireNoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM lexical_record").Scan(&count),
		"joined Host completion: %v")

	if count != 0 {
		t.Fatal("Host success left target material")
	}

	result, err = session.CallTool(t.Context(), params)
	requireNoError(t, err, "HTTP MCP retry: %v")
	encoded, err = json.Marshal(result.StructuredContent)
	requireNoError(t, err, "success envelope: %v")

	if result.IsError || !strings.Contains(string(encoded), outcome.OperationID) ||
		strings.Contains(string(encoded), "partial") {
		t.Fatalf("changed public replay shape: %s", encoded)
	}
}
