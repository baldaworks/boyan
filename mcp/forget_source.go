package mcp

import (
	"context"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Service) forgetSource(
	ctx context.Context,
	_ *sdkmcp.CallToolRequest,
	in forgetSourceServiceInput,
) (
	*sdkmcp.CallToolResult,
	ForgetOutput,
	error,
) {
	scope, err := parseScopeInput(in.ScopeInput)
	if err != nil {
		return s.forgetFailure(
			ForgetSourceToolName,
			classifyErrorCode(err, memory.CodeInvalidScope),
			publicErrorMessage(err),
		)
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
			Scope:          scope,
			ExportID:       strings.TrimSpace(in.ExportID),
			ConversationID: strings.TrimSpace(in.ConversationID),
			SourceTurnID:   strings.TrimSpace(in.SourceTurnID),
		},
		ForgottenAt: forgottenAt,
	}
	if err := command.Validate(); err != nil {
		return s.forgetFailure(
			ForgetSourceToolName,
			classifyErrorCode(err, memory.CodeInvalidDerived),
			publicErrorMessage(err),
		)
	}

	forgetCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	outcome, err := s.forgetter.ForgetSource(forgetCtx, command)
	if err != nil {
		code := classifyProviderError(forgetCtx, err)

		return s.forgetFailure(ForgetSourceToolName, code, publicErrorMessageForCode(code))
	}

	return s.completeForget(ForgetSourceToolName, scope, outcome, forgottenAt)
}
