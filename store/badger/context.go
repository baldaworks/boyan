package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func canonicalContextError(ctx context.Context) error {
	if ctx == nil {
		return memory.PermanentError(memory.CodeInvalidDerived, "context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return memory.RetryableError(memory.CodeTimeout, "canonical store operation canceled", err)
	}
	return nil
}
