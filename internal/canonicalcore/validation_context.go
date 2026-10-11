package canonicalcore

import (
	"context"
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return invalidDerived("context is required")
	}

	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return memory.RetryableError(memory.CodeTimeout, "derived memory operation timed out", nil)
		}

		return memory.RetryableError(memory.CodeTimeout, "derived memory operation was canceled", nil)
	}

	return nil
}
