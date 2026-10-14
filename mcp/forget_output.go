package mcp

import (
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func forgetSuccess(outcome memory.ForgetOutcome, fallbackForgottenAt time.Time) ForgetOutput {
	scope := outcome.Scope
	forgottenAt := outcome.ForgottenAt

	if forgottenAt.IsZero() {
		// Outcomes written before forgotten_at was added remain readable. Their
		// acknowledgement uses the current request timestamp as a compatibility fallback.
		forgottenAt = fallbackForgottenAt
	}

	return ForgetOutput{
		ToolOutcome:         ToolOutcome{OK: true},
		Scope:               &scope,
		OperationID:         outcome.OperationID,
		Kind:                outcome.Kind,
		ScopeVersion:        outcome.ScopeVersion,
		SourceCount:         outcome.SourceCount,
		RevisionCount:       outcome.RevisionCount,
		ReferencesTruncated: outcome.ReferencesTruncated,
		ForgottenAt:         &forgottenAt,
		Sources:             append([]memory.SourceRef(nil), outcome.Sources...),
		Revisions:           append([]memory.RevisionRef(nil), outcome.Revisions...),
		Notice:              "Forget outcomes are content-free durable acknowledgements. No recalled content is returned.",
	}
}

func (s *Service) completeForget(
	toolName string,
	scope memory.Scope,
	expectedKind memory.ForgetKind,
	expectedSource memory.SourceRef,
	outcome memory.ForgetOutcome,
	forgottenAt time.Time,
) (*sdkmcp.CallToolResult, ForgetOutput, error) {
	if err := outcome.Validate(); err != nil {
		code := classifyErrorCode(err, memory.CodePermanent)

		return s.forgetFailure(toolName, code, publicErrorMessageForCode(code))
	}

	if outcome.Scope != scope {
		return s.forgetFailure(toolName, memory.CodeScopeViolation, messageScopeViolation)
	}

	if outcome.Kind != expectedKind ||
		(expectedKind == memory.ForgetKindSource && outcome.Sources[0] != expectedSource) {
		return s.forgetFailure(toolName, memory.CodeInvalidDerived, publicErrorMessageForCode(memory.CodeInvalidDerived))
	}

	return nil, forgetSuccess(outcome, forgottenAt), nil
}
