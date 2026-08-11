package boundarycore

import (
	"context"
	"errors"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func checkBoundaryContext(ctx context.Context) error {
	if ctx == nil {
		return invalidDerived("context is required")
	}
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return RetryableError(CodeTimeout, "derived memory operation timed out", nil)
		}
		return RetryableError(CodeTimeout, "derived memory operation was canceled", nil)
	}
	return nil
}

func boundaryStoreFailure(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx != nil && ctx.Err() != nil {
		return checkBoundaryContext(ctx)
	}
	if code, class, ok := ClassifyError(err); ok {
		return newError(code, class, "derived memory store operation failed", nil)
	}
	return RetryableError(CodeStoreFailure, "derived memory store operation failed", nil)
}

func boundaryModelFailure(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx != nil && ctx.Err() != nil {
		return checkBoundaryContext(ctx)
	}
	if code, class, ok := ClassifyError(err); ok {
		return newError(code, class, "derived memory model operation failed", nil)
	}
	return RetryableError(CodeModelFailure, "derived memory model operation failed", nil)
}
