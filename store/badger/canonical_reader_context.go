package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func canonicalReaderContext(ctx context.Context) error {
	if ctx == nil {
		return memory.PermanentError(memory.CodeInvalidDerived, "canonical reader context is required", nil)
	}
	select {
	case <-ctx.Done():
		return memory.RetryableError(memory.CodeTimeout, "canonical reader context ended", ctx.Err())
	default:
		return nil
	}
}

func isCanonicalMissing(err error) bool {
	code, _, ok := memory.ClassifyError(err)
	return ok && (code == memory.CodeNotFound || code == memory.CodeForgotten)
}
