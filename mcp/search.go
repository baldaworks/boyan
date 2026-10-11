package mcp

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Service) search(
	ctx context.Context,
	_ *sdkmcp.CallToolRequest,
	in searchServiceInput,
) (
	*sdkmcp.CallToolResult,
	SearchOutput,
	error,
) {
	scope, err := parseScopeInput(in.ScopeInput)
	if err != nil {
		return s.searchFailure(classifyErrorCode(err, memory.CodeInvalidScope), publicErrorMessage(err))
	}

	query, limit, err := normalizeSearchInput(in)
	if err != nil {
		return s.searchFailure(classifyErrorCode(err, memory.CodeInvalidQuery), publicErrorMessage(err))
	}

	if s == nil || !s.enabled {
		return s.searchFailure(memory.CodeDisabled, messageDisabled)
	}

	if s.searcher == nil {
		return s.searchFailure(memory.CodeUnavailable, messageUnavailable)
	}

	if ctx == nil {
		ctx = context.Background()
	}

	searchCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	searchRequest, err := buildSearchRequest(in, scope, query, limit)
	if err != nil {
		return s.searchFailure(classifyErrorCode(err, memory.CodeInvalidQuery), publicErrorMessage(err))
	}

	response, err := s.searcher.Search(searchCtx, searchRequest)
	if err != nil {
		code := classifyProviderError(searchCtx, err)

		return s.searchFailure(code, publicErrorMessageForCode(code))
	}

	if err := validateSearchResponse(searchRequest, response); err != nil {
		code := classifyErrorCode(err, memory.CodePermanent)

		return s.searchFailure(code, publicErrorMessageForCode(code))
	}

	return nil, buildSearchOutput(response), nil
}
