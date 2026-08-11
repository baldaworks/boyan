package mcp

import (
	"context"
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

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
