package semanticprojection

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/baldaworks/boyan/internal/embedding"
	semantic "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

func normalizeHybridRequest(ctx context.Context, request memory.RecallRequest) (memory.RecallRequest, error) {
	if ctx == nil {
		return memory.RecallRequest{}, memory.PermanentError(memory.CodeInvalidQuery, "recall context is required", nil)
	}

	if err := ctx.Err(); err != nil {
		return memory.RecallRequest{}, err
	}

	normalized, err := memory.NormalizeRecallRequest(request)
	if err != nil {
		return memory.RecallRequest{}, err
	}

	if !utf8.ValidString(normalized.Query) {
		return memory.RecallRequest{}, memory.PermanentError(memory.CodeInvalidQuery, "recall query is invalid", nil)
	}

	return normalized, nil
}

func safeRecallReason(err error) string {
	if reason, ok := semantic.Classify(err); ok {
		switch reason {
		case semantic.ReasonInvalidInput, semantic.ReasonInputLimit:
			return "invalid_response"
		default:
			return string(reason)
		}
	}

	var providerError *embedding.Error
	if errors.As(err, &providerError) {
		report := memory.RetrievalReport{Effective: "degraded", Reason: string(providerError.Code)}
		if report.Validate() == nil {
			return report.Reason
		}
	}

	return "unavailable"
}
