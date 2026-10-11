package bleve

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func canonicalContextError(ctx context.Context) error {
	if ctx == nil {
		return memory.PermanentError(memory.CodeInvalidDerived, "context is required", nil)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
