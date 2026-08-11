package mcp

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Service) searchFailure(code memory.ErrorCode, message string) (*sdkmcp.CallToolResult, SearchOutput, error) {
	result, outcome := s.failureForTool(SearchToolName, code, message)
	return result, SearchOutput{
		ToolOutcome:        outcome,
		Results:            []Reference{},
		DataClassification: DataClassificationNone,
		Notice:             "No recalled text was returned.",
	}, nil
}

func (s *Service) traceFailure(code memory.ErrorCode, message string) (*sdkmcp.CallToolResult, TraceOutput, error) {
	result, outcome := s.failureForTool(TraceToolName, code, message)
	return result, TraceOutput{
		ToolOutcome:        outcome,
		Revisions:          []memory.SearchHit{},
		Sources:            []memory.SourceRecord{},
		DataClassification: DataClassificationNone,
		Notice:             "No provenance content was returned.",
	}, nil
}

func (s *Service) forgetFailure(toolName string, code memory.ErrorCode, message string) (*sdkmcp.CallToolResult, ForgetOutput, error) {
	result, outcome := s.failureForTool(toolName, code, message)
	return result, ForgetOutput{
		ToolOutcome: outcome,
		Notice:      "No forget mutation was acknowledged.",
	}, nil
}
