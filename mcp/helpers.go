package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func parseScopeInput(in ScopeInput) (memory.Scope, error) {
	scope := memory.Scope{
		Key:  strings.TrimSpace(in.ScopeKey),
		Kind: in.ScopeKind,
	}
	if err := scope.Validate(); err != nil {
		return memory.Scope{}, err
	}
	return scope, nil
}

func normalizeSearchInput(in SearchInput) (string, int, error) {
	query := strings.TrimSpace(in.Query)
	if query == "" || len(query) > memory.MaxSearchQueryBytes {
		return "", 0, memory.PermanentError(memory.CodeInvalidQuery, messageInvalidInput, nil)
	}
	limit := in.Limit
	if limit == 0 {
		limit = memory.DefaultSearchLimit
	}
	if limit < 1 || limit > memory.MaxSearchLimit {
		return "", 0, memory.PermanentError(memory.CodeInvalidQuery, messageInvalidInput, nil)
	}
	if in.MemoryKind != nil && *in.MemoryKind != memory.MemoryKindState && *in.MemoryKind != memory.MemoryKindEvent {
		return "", 0, memory.PermanentError(memory.CodeInvalidQuery, messageInvalidInput, nil)
	}
	if in.Category != nil {
		if err := in.Category.Validate(); err != nil {
			return "", 0, memory.PermanentError(memory.CodeInvalidQuery, messageInvalidInput, nil)
		}
	}
	for _, value := range []string{in.SourceID, in.SessionID, in.MemoryKey} {
		if strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\t") {
			return "", 0, memory.PermanentError(memory.CodeInvalidQuery, messageInvalidInput, nil)
		}
	}
	if in.AsOf != nil && in.AsOf.IsZero() {
		return "", 0, memory.PermanentError(memory.CodeInvalidQuery, messageInvalidInput, nil)
	}
	return query, limit, nil
}

func cloneMemoryKind(value *memory.MemoryKind) *memory.MemoryKind {
	if value == nil {
		return nil
	}
	copyOfValue := *value
	return &copyOfValue
}

func cloneCategory(value *memory.AtomCategory) *memory.AtomCategory {
	if value == nil {
		return nil
	}
	copyOfValue := *value
	return &copyOfValue
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copyOfValue := *value
	return &copyOfValue
}

func copyRecallReferences(results []memory.RecallReference) []Reference {
	out := make([]Reference, 0, len(results))
	for _, result := range results {
		category := cloneCategory(result.Category)
		explain := result.Explain
		evidence := append([]memory.EvidenceRef(nil), result.Evidence...)
		out = append(out, Reference{
			ID:             result.RevisionID,
			ScopeKey:       result.Scope.Key,
			Text:           result.Text,
			CreatedAt:      result.CreatedAt,
			Score:          floatPointer(result.Score),
			MemoryKind:     result.Kind,
			ItemID:         result.ItemID,
			RevisionID:     result.RevisionID,
			Revision:       result.Revision,
			State:          result.State,
			Category:       category,
			MemoryKey:      result.MemoryKey,
			Evidence:       evidence,
			Explain:        &explain,
			ScopeChangeSeq: result.ScopeChangeSeq,
		})
	}
	return out
}

func floatPointer(value float64) *float64 {
	copyOfValue := value
	return &copyOfValue
}

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

func (s *Service) failureForTool(toolName string, code memory.ErrorCode, message string) (*sdkmcp.CallToolResult, ToolOutcome) {
	if code == "" {
		code = memory.CodePermanent
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = publicErrorMessageForCode(code)
	}
	return &sdkmcp.CallToolResult{
			IsError: true,
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: fmt.Sprintf("%s: %s", toolName, message)}},
		}, ToolOutcome{
			OK: false,
			Error: &ToolError{
				Operation: toolName,
				Code:      string(code),
				Message:   fmt.Sprintf("%s: %s", toolName, message),
			},
		}
}

func classifyProviderError(ctx context.Context, err error) memory.ErrorCode {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return memory.CodeTimeout
	}
	if errors.Is(err, context.Canceled) {
		return memory.CodeShuttingDown
	}
	return classifyErrorCode(err, memory.CodeUnavailable)
}

func classifyErrorCode(err error, fallback memory.ErrorCode) memory.ErrorCode {
	if code, _, ok := memory.ClassifyError(err); ok {
		return code
	}
	return fallback
}

func publicErrorMessage(err error) string {
	return publicErrorMessageForCode(classifyErrorCode(err, memory.CodePermanent))
}

func publicErrorMessageForCode(code memory.ErrorCode) string {
	switch code {
	case memory.CodeInvalidQuery, memory.CodeInvalidDerived:
		return messageInvalidInput
	case memory.CodeDisabled:
		return messageDisabled
	case memory.CodeInvalidScope:
		return messageInvalidScope
	case memory.CodeUnsupportedScope:
		return messageUnsupported
	case memory.CodeUnavailable:
		return messageUnavailable
	case memory.CodeTimeout:
		return messageTimeout
	case memory.CodeScopeViolation:
		return messageScopeViolation
	case memory.CodeShuttingDown:
		return messageShuttingDown
	case memory.CodeConflict:
		return messageConflict
	case memory.CodeLimitExceeded:
		return messageBounded
	default:
		return messagePermanent
	}
}
