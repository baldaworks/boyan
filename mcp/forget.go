package mcp

import (
	"context"
	"strings"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Service) forgetSource(ctx context.Context, _ *sdkmcp.CallToolRequest, in forgetSourceServiceInput) (*sdkmcp.CallToolResult, ForgetOutput, error) {
	scope, err := parseScopeInput(in.ScopeInput)
	if err != nil {
		return s.forgetFailure(ForgetSourceToolName, classifyErrorCode(err, memory.CodeInvalidScope), publicErrorMessage(err))
	}
	if s == nil || !s.enabled {
		return s.forgetFailure(ForgetSourceToolName, memory.CodeDisabled, messageDisabled)
	}
	if s.forgetter == nil {
		return s.forgetFailure(ForgetSourceToolName, memory.CodeUnavailable, messageUnavailable)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	forgottenAt := s.now().UTC()
	command := memory.ForgetSourceCommand{
		SchemaVersion: memory.DerivedSchemaVersionV1,
		Source: memory.SourceRef{
			Scope:        scope,
			ExportID:     strings.TrimSpace(in.ExportID),
			SessionID:    strings.TrimSpace(in.SessionID),
			SourceTurnID: strings.TrimSpace(in.SourceTurnID),
		},
		ForgottenAt: forgottenAt,
	}
	if err := command.Validate(); err != nil {
		return s.forgetFailure(ForgetSourceToolName, classifyErrorCode(err, memory.CodeInvalidDerived), publicErrorMessage(err))
	}
	forgetCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	outcome, err := s.forgetter.ForgetSource(forgetCtx, command)
	if err != nil {
		code := classifyProviderError(forgetCtx, err)
		return s.forgetFailure(ForgetSourceToolName, code, publicErrorMessageForCode(code))
	}
	if err := outcome.Validate(); err != nil {
		code := classifyErrorCode(err, memory.CodePermanent)
		return s.forgetFailure(ForgetSourceToolName, code, publicErrorMessageForCode(code))
	}
	if outcome.Scope != scope {
		return s.forgetFailure(ForgetSourceToolName, memory.CodeScopeViolation, messageScopeViolation)
	}
	return nil, forgetSuccess(outcome, forgottenAt), nil
}

func (s *Service) forgetScope(ctx context.Context, _ *sdkmcp.CallToolRequest, in forgetScopeServiceInput) (*sdkmcp.CallToolResult, ForgetOutput, error) {
	scope, err := parseScopeInput(in.ScopeInput)
	if err != nil {
		return s.forgetFailure(ForgetScopeToolName, classifyErrorCode(err, memory.CodeInvalidScope), publicErrorMessage(err))
	}
	if strings.TrimSpace(in.ConfirmScopeKey) != scope.Key {
		return s.forgetFailure(ForgetScopeToolName, memory.CodeInvalidDerived, messageInvalidInput)
	}
	if s == nil || !s.enabled {
		return s.forgetFailure(ForgetScopeToolName, memory.CodeDisabled, messageDisabled)
	}
	if s.forgetter == nil {
		return s.forgetFailure(ForgetScopeToolName, memory.CodeUnavailable, messageUnavailable)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	forgottenAt := s.now().UTC()
	command := memory.ForgetScopeCommand{
		SchemaVersion: memory.DerivedSchemaVersionV1,
		Scope:         scope,
		RequestID:     strings.TrimSpace(in.RequestID),
		ForgottenAt:   forgottenAt,
	}
	if err := command.Validate(); err != nil {
		return s.forgetFailure(ForgetScopeToolName, classifyErrorCode(err, memory.CodeInvalidDerived), publicErrorMessage(err))
	}
	forgetCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	outcome, err := s.forgetter.ForgetScope(forgetCtx, command)
	if err != nil {
		code := classifyProviderError(forgetCtx, err)
		return s.forgetFailure(ForgetScopeToolName, code, publicErrorMessageForCode(code))
	}
	if err := outcome.Validate(); err != nil {
		code := classifyErrorCode(err, memory.CodePermanent)
		return s.forgetFailure(ForgetScopeToolName, code, publicErrorMessageForCode(code))
	}
	if outcome.Scope != scope {
		return s.forgetFailure(ForgetScopeToolName, memory.CodeScopeViolation, messageScopeViolation)
	}
	return nil, forgetSuccess(outcome, forgottenAt), nil
}

func forgetSuccess(outcome memory.ForgetOutcome, forgottenAt time.Time) ForgetOutput {
	scope := outcome.Scope
	return ForgetOutput{
		ToolOutcome:  ToolOutcome{OK: true},
		Scope:        &scope,
		OperationID:  outcome.OperationID,
		Kind:         outcome.Kind,
		ScopeVersion: outcome.ScopeVersion,
		ForgottenAt:  &forgottenAt,
		Sources:      append([]memory.SourceRef(nil), outcome.Sources...),
		Revisions:    append([]memory.RevisionRef(nil), outcome.Revisions...),
		Notice:       "Forget outcomes are content-free durable acknowledgements. No recalled content is returned.",
	}
}
