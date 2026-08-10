package mcp

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type recallSearcherFunc func(context.Context, boyan.RecallRequest) (boyan.RecallResponse, error)

func (f recallSearcherFunc) Search(ctx context.Context, request boyan.RecallRequest) (boyan.RecallResponse, error) {
	return f(ctx, request)
}

type traceReaderFunc func(context.Context, boyan.TraceRequest) (boyan.TraceResponse, error)

func (f traceReaderFunc) Trace(ctx context.Context, request boyan.TraceRequest) (boyan.TraceResponse, error) {
	return f(ctx, request)
}

type forgetterSpy struct {
	sourceCommand boyan.ForgetSourceCommand
	scopeCommand  boyan.ForgetScopeCommand
	sourceOutcome boyan.ForgetOutcome
	scopeOutcome  boyan.ForgetOutcome
	sourceErr     error
	scopeErr      error
}

func (f *forgetterSpy) ForgetSource(_ context.Context, command boyan.ForgetSourceCommand) (boyan.ForgetOutcome, error) {
	f.sourceCommand = command
	return f.sourceOutcome, f.sourceErr
}

func (f *forgetterSpy) ForgetScope(_ context.Context, command boyan.ForgetScopeCommand) (boyan.ForgetOutcome, error) {
	f.scopeCommand = command
	return f.scopeOutcome, f.scopeErr
}

func TestSearchUsesExplicitScopeAndReturnsBoundedReference(t *testing.T) {
	t.Parallel()

	scope := boyan.Scope{Key: "telegram:1:0", Kind: boyan.ScopeKindPersonal}
	var received boyan.RecallRequest
	service := New(Config{
		Enabled: true,
		Searcher: recallSearcherFunc(func(_ context.Context, request boyan.RecallRequest) (boyan.RecallResponse, error) {
			received = request
			return boyan.RecallResponse{
				SchemaVersion: boyan.RecallSchemaVersionV1,
				Trust:         boyan.ReferenceTrustUntrusted,
				Scope:         scope,
				Results: []boyan.RecallReference{{
					SchemaVersion: boyan.RecallSchemaVersionV1,
					Trust:         boyan.ReferenceTrustUntrusted,
					Scope:         scope,
					ItemID:        "item-1",
					RevisionID:    "revision-1",
					Revision:      1,
					Kind:          boyan.MemoryKindState,
					State:         boyan.RevisionStateActive,
					Text:          "untrusted reference",
					CreatedAt:     time.Unix(1, 0).UTC(),
				}},
			}, nil
		}),
	})
	_, output, err := service.search(context.Background(), nil, SearchInput{
		ScopeInput: ScopeInput{ScopeKey: scope.Key, ScopeKind: scope.Kind},
		Query:      "reference",
		Limit:      1,
	})
	if err != nil || !output.OK {
		t.Fatalf("search() = output %#v, error %v", output, err)
	}
	if received.Scope != scope || received.Query != "reference" || received.Limit != 1 {
		t.Fatalf("search request = %#v", received)
	}
	if len(output.Results) != 1 || output.Results[0].Text != "untrusted reference" {
		t.Fatalf("search output = %#v", output.Results)
	}
	if output.DataClassification != DataClassificationUntrustedReference || output.Scope == nil || *output.Scope != scope {
		t.Fatalf("search classification/scope = %#v", output)
	}
}

func TestSearchPreservesExactScopeKind(t *testing.T) {
	t.Parallel()

	scope := boyan.Scope{Key: "telegram:shared:42", Kind: boyan.ScopeKindGroup}
	var received boyan.RecallRequest
	service := New(Config{
		Enabled: true,
		Searcher: recallSearcherFunc(func(_ context.Context, request boyan.RecallRequest) (boyan.RecallResponse, error) {
			received = request
			return boyan.RecallResponse{
				SchemaVersion: boyan.RecallSchemaVersionV1,
				Trust:         boyan.ReferenceTrustUntrusted,
				Scope:         scope,
				Results:       []boyan.RecallReference{},
			}, nil
		}),
	})
	_, output, err := service.search(context.Background(), nil, SearchInput{
		ScopeInput: ScopeInput{ScopeKey: scope.Key, ScopeKind: scope.Kind},
		Query:      "scope",
	})
	if err != nil || !output.OK {
		t.Fatalf("search() error = %v output=%#v", err, output)
	}
	if received.Scope != scope {
		t.Fatalf("received scope = %#v, want %#v", received.Scope, scope)
	}
}

func TestSearchRejectsForeignReturnedScope(t *testing.T) {
	t.Parallel()

	scope := boyan.Scope{Key: "telegram:1:0", Kind: boyan.ScopeKindPersonal}
	foreign := boyan.Scope{Key: "telegram:-100:1", Kind: boyan.ScopeKindGroup}
	service := New(Config{
		Enabled: true,
		Searcher: recallSearcherFunc(func(context.Context, boyan.RecallRequest) (boyan.RecallResponse, error) {
			return boyan.RecallResponse{
				SchemaVersion: boyan.RecallSchemaVersionV1,
				Trust:         boyan.ReferenceTrustUntrusted,
				Scope:         foreign,
			}, nil
		}),
	})
	result, output, err := service.search(context.Background(), nil, SearchInput{
		ScopeInput: ScopeInput{ScopeKey: scope.Key, ScopeKind: scope.Kind},
		Query:      "secret",
	})
	if err != nil || result == nil || !result.IsError || output.OK {
		t.Fatalf("foreign search = result %#v output %#v error %v", result, output, err)
	}
	if output.Error == nil || output.Error.Code != string(boyan.CodeScopeViolation) {
		t.Fatalf("foreign search error = %#v", output.Error)
	}
}

func TestTraceUsesExplicitScopeAndReturnsUntrustedGraph(t *testing.T) {
	t.Parallel()

	scope := boyan.Scope{Key: "telegram:trace", Kind: boyan.ScopeKindPersonal}
	root := validTraceRoot(t, scope)
	var received boyan.TraceRequest
	service := New(Config{
		Enabled: true,
		Tracer: traceReaderFunc(func(_ context.Context, request boyan.TraceRequest) (boyan.TraceResponse, error) {
			received = request
			return traceResponse(scope, root), nil
		}),
	})
	_, output, err := service.trace(context.Background(), nil, TraceInput{
		ScopeInput: ScopeInput{ScopeKey: scope.Key, ScopeKind: scope.Kind},
		ItemID:     root.ItemID,
		RevisionID: root.RevisionID,
		MaxNodes:   8,
	})
	if err != nil || !output.OK {
		t.Fatalf("trace() error = %v output=%#v", err, output)
	}
	if received.Scope != scope || received.Root != root || received.MaxNodes != 8 {
		t.Fatalf("trace request = %#v", received)
	}
	if output.Scope == nil || *output.Scope != scope || output.Root == nil || *output.Root != root {
		t.Fatalf("trace output = %#v", output)
	}
	if output.DataClassification != DataClassificationUntrustedReference {
		t.Fatalf("trace classification = %q", output.DataClassification)
	}
}

func TestForgetSourceUsesServerOwnedTimestampAndContentFreeOutcome(t *testing.T) {
	t.Parallel()

	scope := boyan.Scope{Key: "telegram:forget-source", Kind: boyan.ScopeKindPersonal}
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	backend := &forgetterSpy{
		sourceOutcome: boyan.ForgetOutcome{
			SchemaVersion: boyan.DerivedSchemaVersionV1,
			OperationID:   "forget-op-1",
			Kind:          boyan.ForgetKindSource,
			Scope:         scope,
			ScopeVersion:  4,
			Sources: []boyan.SourceRef{{
				Scope:        scope,
				ExportID:     "export-1",
				SessionID:    "session-1",
				SourceTurnID: "turn-1",
			}},
		},
	}
	service := New(Config{Enabled: true, Forgetter: backend, Now: func() time.Time { return now }})

	_, output, err := service.forgetSource(context.Background(), nil, ForgetSourceInput{
		ScopeInput:   ScopeInput{ScopeKey: scope.Key, ScopeKind: scope.Kind},
		ExportID:     "export-1",
		SessionID:    "session-1",
		SourceTurnID: "turn-1",
	})
	if err != nil || !output.OK {
		t.Fatalf("forgetSource() error = %v output=%#v", err, output)
	}
	if backend.sourceCommand.ForgottenAt != now || backend.sourceCommand.Source.Scope != scope {
		t.Fatalf("source command = %#v", backend.sourceCommand)
	}
	if output.ForgottenAt == nil || !output.ForgottenAt.Equal(now) {
		t.Fatalf("forgotten_at = %#v, want %v", output.ForgottenAt, now)
	}
	if len(output.Sources) != 1 || len(output.Revisions) != 0 || output.OperationID != "forget-op-1" {
		t.Fatalf("forget output = %#v", output)
	}
}

func TestForgetScopeRequiresConfirmation(t *testing.T) {
	t.Parallel()

	scope := boyan.Scope{Key: "telegram:forget-scope", Kind: boyan.ScopeKindPersonal}
	service := New(Config{Enabled: true, Forgetter: &forgetterSpy{}})
	result, output, err := service.forgetScope(context.Background(), nil, ForgetScopeInput{
		ScopeInput:      ScopeInput{ScopeKey: scope.Key, ScopeKind: scope.Kind},
		RequestID:       "request-1",
		ConfirmScopeKey: "telegram:other",
	})
	if err != nil || result == nil || !result.IsError || output.OK {
		t.Fatalf("forgetScope() mismatch = result %#v output %#v error %v", result, output, err)
	}
	if output.Error == nil || output.Error.Code != string(boyan.CodeInvalidDerived) {
		t.Fatalf("forgetScope() error = %#v", output.Error)
	}
}

func TestMCPContractUsesBoyanNamesAndExplicitScopeFields(t *testing.T) {
	t.Parallel()

	if SearchToolName != "boyan.search" || TraceToolName != "boyan.trace" || ForgetSourceToolName != "boyan.forget_source" || ForgetScopeToolName != "boyan.forget_scope" {
		t.Fatalf("tool names = %q/%q/%q/%q", SearchToolName, TraceToolName, ForgetSourceToolName, ForgetScopeToolName)
	}
	typeOfSearch := reflect.TypeOf(SearchInput{})
	for _, required := range []string{"ScopeKey", "ScopeKind"} {
		if _, ok := typeOfSearch.FieldByName(required); !ok {
			t.Fatalf("SearchInput is missing %q", required)
		}
	}
	for _, forbidden := range []string{"ScopeResolver", "Locator", "ChannelType", "AddressKey"} {
		if _, ok := typeOfSearch.FieldByName(forbidden); ok {
			t.Fatalf("SearchInput exposes forbidden field %q", forbidden)
		}
	}
}

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
	handler, err := NewHandler(Config{
		Enabled: true,
		Now:     func() time.Time { return forgottenAt },
		Searcher: recallSearcherFunc(func(_ context.Context, request boyan.RecallRequest) (boyan.RecallResponse, error) {
			return boyan.RecallResponse{
				SchemaVersion: boyan.RecallSchemaVersionV1,
				Trust:         boyan.ReferenceTrustUntrusted,
				Scope:         request.Scope,
				Results: []boyan.RecallReference{{
					SchemaVersion: boyan.RecallSchemaVersionV1,
					Trust:         boyan.ReferenceTrustUntrusted,
					Scope:         request.Scope,
					ItemID:        "item-1",
					RevisionID:    "revision-1",
					Revision:      1,
					Kind:          boyan.MemoryKindState,
					State:         boyan.RevisionStateActive,
					Text:          "mcp reference",
					CreatedAt:     time.Unix(1, 0).UTC(),
				}},
			}, nil
		}),
		Tracer: traceReaderFunc(func(_ context.Context, request boyan.TraceRequest) (boyan.TraceResponse, error) {
			return traceResponse(request.Scope, request.Root), nil
		}),
		Forgetter: backend,
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	server := httptest.NewServer(mux)
	defer server.Close()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	session, err := client.Connect(context.Background(), &sdkmcp.StreamableClientTransport{Endpoint: server.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer session.Close()

	toolsResult, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
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

	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: SearchToolName,
		Arguments: map[string]any{
			"scope_key":  scope.Key,
			"scope_kind": scope.Kind,
			"query":      "mcp",
			"limit":      1,
		},
	})
	if err != nil {
		t.Fatalf("CallTool(search) error = %v", err)
	}
	output := decodeStructuredContent[SearchOutput](t, result)
	if !output.OK || output.Scope == nil || *output.Scope != scope || len(output.Results) != 1 || output.Results[0].Text != "mcp reference" {
		t.Fatalf("search output = %#v", output)
	}

	forgetResult, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: ForgetScopeToolName,
		Arguments: map[string]any{
			"scope_key":         scope.Key,
			"scope_kind":        scope.Kind,
			"request_id":        "forget-request-1",
			"confirm_scope_key": scope.Key,
		},
	})
	if err != nil {
		t.Fatalf("CallTool(forget_scope) error = %v", err)
	}
	forgetOutput := decodeStructuredContent[ForgetOutput](t, forgetResult)
	if !forgetOutput.OK || forgetOutput.OperationID != "forget-op-http" || forgetOutput.ForgottenAt == nil || !forgetOutput.ForgottenAt.Equal(forgottenAt) {
		t.Fatalf("forget output = %#v", forgetOutput)
	}
	if backend.scopeCommand.Scope != scope || backend.scopeCommand.RequestID != "forget-request-1" || !backend.scopeCommand.ForgottenAt.Equal(forgottenAt) {
		t.Fatalf("scope command = %#v", backend.scopeCommand)
	}
}

func decodeStructuredContent[T any](t *testing.T, result *sdkmcp.CallToolResult) T {
	t.Helper()
	if result == nil {
		t.Fatalf("CallToolResult is nil")
	}
	var out T
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("json.Marshal(structured content) error = %v", err)
	}
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatalf("json.Unmarshal(structured content) error = %v", err)
	}
	return out
}

func traceResponse(scope boyan.Scope, root boyan.RevisionRef) boyan.TraceResponse {
	sourceRef, turn := validTraceSource(scope)
	return boyan.TraceResponse{
		SchemaVersion: boyan.DerivedSchemaVersionV1,
		Trust:         boyan.ReferenceTrustUntrusted,
		Scope:         scope,
		Root:          root,
		Revisions: []boyan.SearchHit{{
			Atom: &boyan.Atom{
				Meta: boyan.RevisionMeta{
					SchemaVersion: boyan.DerivedSchemaVersionV1,
					Kind:          boyan.DerivedKindAtom,
					ItemID:        root.ItemID,
					RevisionID:    root.RevisionID,
					Revision:      1,
					OperationID:   "trace-op-1",
					Scope:         scope,
					State:         boyan.RevisionStateActive,
					Provenance:    boyan.Provenance{RawSources: []boyan.SourceRef{sourceRef}},
					CreatedAt:     time.Date(2026, time.August, 9, 12, 30, 0, 0, time.UTC),
				},
				Category: boyan.AtomCategoryFact,
				Text:     "trace text",
				Relation: boyan.CandidateRelationNew,
			},
		}},
		Sources: []boyan.SourceRecord{{
			SchemaVersion: boyan.DerivedSchemaVersionV1,
			Ref:           sourceRef,
			State:         boyan.SourceStateActive,
			Turn:          &turn,
		}},
	}
}

func validTraceRoot(t *testing.T, scope boyan.Scope) boyan.RevisionRef {
	t.Helper()
	sourceRef, _ := validTraceSource(scope)
	itemID, err := boyan.AtomItemID(scope, boyan.AtomCategoryFact, "trace text")
	if err != nil {
		t.Fatalf("AtomItemID() error = %v", err)
	}
	revisionID, err := boyan.DerivedRevisionID(
		scope,
		itemID,
		"trace-op-1",
		[]string{
			string(boyan.AtomCategoryFact),
			"trace text",
			string(boyan.CandidateRelationNew),
		},
		boyan.Provenance{RawSources: []boyan.SourceRef{sourceRef}},
		nil,
	)
	if err != nil {
		t.Fatalf("DerivedRevisionID() error = %v", err)
	}
	return boyan.RevisionRef{ItemID: itemID, RevisionID: revisionID}
}

func validTraceSource(scope boyan.Scope) (boyan.SourceRef, boyan.Turn) {
	turn, err := boyan.NewTurn(
		scope,
		boyan.SessionRef{SessionID: "session-trace", AgentSessionID: "agent-session-trace"},
		"turn-trace",
		time.Date(2026, time.August, 9, 12, 15, 0, 0, time.UTC),
		"user trace",
		"assistant trace",
	)
	if err != nil {
		panic(err)
	}
	return boyan.SourceRef{
		Scope:        scope,
		ExportID:     turn.ExportID,
		SessionID:    turn.Session.SessionID,
		SourceTurnID: turn.SourceTurnID,
	}, turn
}
