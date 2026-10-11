package mcp

import (
	"context"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Service) forgetScope(
	ctx context.Context,
	_ *sdkmcp.CallToolRequest,
	in forgetScopeServiceInput,
) (
	*sdkmcp.CallToolResult,
	ForgetOutput,
	error,
) {
	scope, err := parseScopeInput(in.ScopeInput)
	if err != nil {
		return s.forgetFailure(
			ForgetScopeToolName,
			classifyErrorCode(err, memory.CodeInvalidScope),
			publicErrorMessage(err),
		)
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
		return s.forgetFailure(
			ForgetScopeToolName,
			classifyErrorCode(err, memory.CodeInvalidDerived),
			publicErrorMessage(err),
		)
	}

	forgetCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	outcome, err := s.forgetter.ForgetScope(forgetCtx, command)
	if err != nil {
		code := classifyProviderError(forgetCtx, err)

		return s.forgetFailure(ForgetScopeToolName, code, publicErrorMessageForCode(code))
	}

	return s.completeForget(ForgetScopeToolName, scope, outcome, forgottenAt)
}
