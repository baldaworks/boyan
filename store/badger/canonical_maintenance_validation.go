package badger

import (
	"context"
	"io"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func validateCanonicalMaintenance(ctx context.Context, value any) error {
	if err := checkCanonicalMaintenanceContext(ctx); err != nil {
		return err
	}
	if value == nil {
		return memory.PermanentError(memory.CodeInvalidDerived, "canonical maintenance stream is required", nil)
	}
	switch stream := value.(type) {
	case io.Reader:
		if stream == nil {
			return memory.PermanentError(memory.CodeInvalidDerived, "canonical maintenance reader is required", nil)
		}
	case io.Writer:
		if stream == nil {
			return memory.PermanentError(memory.CodeInvalidDerived, "canonical maintenance writer is required", nil)
		}
	default:
		return memory.PermanentError(memory.CodeInvalidDerived, "canonical maintenance stream is invalid", nil)
	}
	return nil
}

func checkCanonicalMaintenanceContext(ctx context.Context) error {
	if ctx == nil {
		return memory.PermanentError(memory.CodeInvalidDerived, "canonical maintenance context is required", nil)
	}
	select {
	case <-ctx.Done():
		return memory.RetryableError(memory.CodeTimeout, "canonical maintenance context ended", ctx.Err())
	default:
		return nil
	}
}
