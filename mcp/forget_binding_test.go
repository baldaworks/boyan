package mcp //nolint:testpackage // Exercises request binding through the registered SDK tools.

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type forgetBindingCase struct {
	name       string
	tool       string
	outcome    memory.ForgetOutcome
	backendErr error
	code       memory.ErrorCode
}

func TestForgetRequestBindingThroughSDK(t *testing.T) {
	t.Parallel()

	for _, test := range forgetBindingCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			runForgetBindingCase(t, test)
		})
	}
}

func TestForgetAcknowledgementUsesDurableOutcomeTimestamp(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	durableTime := time.Date(2026, time.October, 10, 4, 5, 6, 0, time.UTC)
	outcome := forgetBindingOutcome(memory.ForgetKindScope)
	outcome.ForgottenAt = durableTime
	backend := &forgetterSpy{scopeOutcome: outcome}
	invocation := durableTime.Add(24 * time.Hour)
	server, err := NewServer(Config{
		Enabled:   true,
		Forgetter: backend,
		Now: func() time.Time {
			invocation = invocation.Add(time.Hour)
			return invocation
		},
	})
	requireNoError(t, err, "server: %v")

	session := connectSearchWatermarkClient(t, ctx, server)
	for range 2 {
		result, callErr := session.CallTool(ctx, &sdkmcp.CallToolParams{
			Name: ForgetScopeToolName, Arguments: forgetBindingArguments(ForgetScopeToolName),
		})
		requireNoError(t, callErr, "call forget: %v")

		output := decodeStructuredContent[ForgetOutput](t, result)
		if result.IsError || output.ForgottenAt == nil || !output.ForgottenAt.Equal(durableTime) {
			t.Fatalf("forgotten_at = %v, want durable %v", output.ForgottenAt, durableTime)
		}
	}
}

func runForgetBindingCase(t *testing.T, test forgetBindingCase) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	backend := &forgetterSpy{
		sourceOutcome: test.outcome, scopeOutcome: test.outcome,
		sourceErr: test.backendErr, scopeErr: test.backendErr,
	}
	now := time.Date(2026, time.October, 14, 1, 2, 3, 0, time.UTC)
	server, err := NewServer(Config{Enabled: true, Forgetter: backend, Now: func() time.Time { return now }})
	requireNoError(t, err, "server: %v")

	session := connectSearchWatermarkClient(t, ctx, server)
	// Repeat the same request/outcome to preserve idempotent acknowledgements.
	for range 2 {
		result, callErr := session.CallTool(ctx, &sdkmcp.CallToolParams{
			Name: test.tool, Arguments: forgetBindingArguments(test.tool),
		})
		requireNoError(t, callErr, "call forget: %v")

		output := decodeStructuredContent[ForgetOutput](t, result)
		if test.code != "" {
			assertForgetBindingFailure(t, result, output, test.code)
		} else {
			assertForgetBindingSuccess(t, result, output, test.outcome, now)
		}
	}
}

func forgetBindingArguments(tool string) map[string]any {
	args := map[string]any{"scope_key": " test:binding-private ", "scope_kind": memory.ScopeKindPersonal}
	if tool == ForgetSourceToolName {
		args["export_id"] = " private-export "
		args["conversation_id"] = " private-conversation "
		args["source_turn_id"] = " private-turn "
	} else {
		args["confirm_scope_key"] = " test:binding-private "
		args["request_id"] = " portable-request "
	}

	return args
}

func assertForgetBindingSuccess(
	t *testing.T, result *sdkmcp.CallToolResult, output ForgetOutput, outcome memory.ForgetOutcome, now time.Time,
) {
	t.Helper()

	expected := ForgetOutput{
		ToolOutcome: ToolOutcome{OK: true}, Scope: &outcome.Scope, OperationID: "portable-private-operation",
		Kind: outcome.Kind, ScopeVersion: outcome.ScopeVersion, ForgottenAt: &now,
		SourceCount: outcome.SourceCount, RevisionCount: outcome.RevisionCount,
		ReferencesTruncated: outcome.ReferencesTruncated, Sources: outcome.Sources, Revisions: outcome.Revisions,
		Notice: output.Notice,
	}
	if result.IsError || !reflect.DeepEqual(output, expected) {
		t.Fatalf("forget success = %+v, error=%v", output, result.IsError)
	}
}

func assertForgetBindingFailure(
	t *testing.T, result *sdkmcp.CallToolResult, output ForgetOutput, code memory.ErrorCode,
) {
	t.Helper()

	expected := ForgetOutput{ToolOutcome: output.ToolOutcome, Notice: output.Notice}
	if !result.IsError || output.OK || output.Error == nil || !reflect.DeepEqual(output, expected) {
		t.Fatalf("expected safe failure, got %+v, error=%v", output, result.IsError)
	}

	if output.Error.Code != string(code) {
		t.Fatalf("expected %s, got %+v", code, output.Error)
	}

	encoded, err := json.Marshal(result)
	requireNoError(t, err, "encode result: %v")

	for _, private := range []string{"test:binding-private", "private-export", "private-conversation", "private-turn",
		"portable-private-operation", "foreign-private", "private-provider-error"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("forget failure leaked outcome: %s", encoded)
		}
	}
}

func forgetBindingOutcome(kind memory.ForgetKind) memory.ForgetOutcome {
	scope := memory.Scope{Key: "test:binding-private", Kind: memory.ScopeKindPersonal}

	outcome := memory.ForgetOutcome{
		SchemaVersion: memory.DerivedSchemaVersionV1, OperationID: "portable-private-operation",
		Kind: kind, Scope: scope, ScopeVersion: 7,
	}
	if kind == memory.ForgetKindSource {
		outcome.Sources = []memory.SourceRef{{
			Scope: scope, ExportID: "private-export", ConversationID: "private-conversation", SourceTurnID: "private-turn",
		}}
	}

	return outcome
}

func forgetBindingCases() []forgetBindingCase {
	cases := []forgetBindingCase{
		{name: "source/matching", tool: ForgetSourceToolName, outcome: forgetBindingOutcome(memory.ForgetKindSource)},
		{name: "scope/matching-empty", tool: ForgetScopeToolName, outcome: forgetBindingOutcome(memory.ForgetKindScope)},
		{name: "source/wrong-kind", tool: ForgetSourceToolName, outcome: forgetBindingOutcome(memory.ForgetKindScope),
			code: memory.CodeInvalidDerived},
		{name: "scope/wrong-kind", tool: ForgetScopeToolName, outcome: forgetBindingOutcome(memory.ForgetKindSource),
			code: memory.CodeInvalidDerived},
	}
	cases = append(cases, forgetBindingSourceCases()...)
	cases = append(cases, forgetBindingValidationCases()...)

	summary := forgetBindingOutcome(memory.ForgetKindScope)
	summary.ReferencesTruncated = true
	summary.SourceCount = memory.MaxForgetOutcomeReferences + 1
	summary.RevisionCount = memory.MaxForgetOutcomeReferences + 2
	cases = append(cases, forgetBindingCase{name: "scope/truncated", tool: ForgetScopeToolName, outcome: summary})

	exhaustive := forgetBindingOutcome(memory.ForgetKindSource)
	exhaustive.Kind = memory.ForgetKindScope
	exhaustive.SourceCount = 1
	exhaustive.RevisionCount = 1
	exhaustive.Revisions = []memory.RevisionRef{{ItemID: "item", RevisionID: "revision"}}

	return append(cases, forgetBindingCase{name: "scope/exhaustive", tool: ForgetScopeToolName, outcome: exhaustive})
}

func forgetBindingSourceCases() []forgetBindingCase {
	var cases []forgetBindingCase

	for _, field := range []string{"export", "conversation", "turn", "scope-key", "scope-kind"} {
		outcome := forgetBindingOutcome(memory.ForgetKindSource)
		code := memory.CodeInvalidDerived

		switch field {
		case "export":
			outcome.Sources[0].ExportID = "foreign-private-export"
		case "conversation":
			outcome.Sources[0].ConversationID = "foreign-private-conversation"
		case "turn":
			outcome.Sources[0].SourceTurnID = "foreign-private-turn"
		case "scope-key":
			outcome.Sources[0].Scope.Key = "test:foreign-private-scope"
			code = memory.CodeScopeViolation
		case "scope-kind":
			outcome.Sources[0].Scope.Kind = memory.ScopeKindGroup
			code = memory.CodeScopeViolation
		}

		cases = append(cases, forgetBindingCase{
			name: "source/changed-" + field, tool: ForgetSourceToolName, outcome: outcome, code: code,
		})
	}

	return cases
}

func forgetBindingValidationCases() []forgetBindingCase {
	var cases []forgetBindingCase

	for _, tool := range []string{ForgetSourceToolName, ForgetScopeToolName} {
		kind := memory.ForgetKindScope
		if tool == ForgetSourceToolName {
			kind = memory.ForgetKindSource
		}

		malformed := forgetBindingOutcome(kind)
		malformed.ScopeVersion = 0
		foreign := forgetBindingOutcome(kind)

		foreign.Scope.Key = "test:foreign-private-scope"
		if kind == memory.ForgetKindSource {
			foreign.Sources[0].Scope = foreign.Scope
		}

		cases = append(cases,
			forgetBindingCase{name: tool + "/malformed", tool: tool, outcome: malformed, code: memory.CodeInvalidDerived},
			forgetBindingCase{name: tool + "/foreign-scope", tool: tool, outcome: foreign, code: memory.CodeScopeViolation},
			forgetBindingCase{name: tool + "/provider-error", tool: tool, outcome: forgetBindingOutcome(kind),
				backendErr: errors.New("private-provider-error"), code: memory.CodeUnavailable},
		)
	}

	return cases
}
