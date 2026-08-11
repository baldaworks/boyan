package mcp

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Service) trace(ctx context.Context, _ *sdkmcp.CallToolRequest, in traceServiceInput) (*sdkmcp.CallToolResult, TraceOutput, error) {
	scope, err := parseScopeInput(in.ScopeInput)
	if err != nil {
		return s.traceFailure(classifyErrorCode(err, memory.CodeInvalidScope), publicErrorMessage(err))
	}
	if s == nil || !s.enabled {
		return s.traceFailure(memory.CodeDisabled, messageDisabled)
	}
	if s.tracer == nil {
		return s.traceFailure(memory.CodeUnavailable, messageUnavailable)
	}
	root, err := normalizeTraceRoot(in)
	if err != nil {
		return s.traceFailure(classifyErrorCode(err, memory.CodeInvalidQuery), messageInvalidInput)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	traceRequest, err := buildTraceRequest(in, scope, root)
	if err != nil {
		return s.traceFailure(classifyErrorCode(err, memory.CodeInvalidQuery), publicErrorMessage(err))
	}
	traceCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	response, err := s.tracer.Trace(traceCtx, traceRequest)
	if err != nil {
		code := classifyProviderError(traceCtx, err)
		return s.traceFailure(code, publicErrorMessageForCode(code))
	}
	if err := validateTraceResponse(traceRequest, response); err != nil {
		code := classifyErrorCode(err, memory.CodePermanent)
		return s.traceFailure(code, publicErrorMessageForCode(code))
	}
	return nil, buildTraceOutput(response), nil
}
