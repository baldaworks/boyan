package badger

import (
	"context"

	"github.com/baldaworks/boyan"
)

func sessionMemoryContextError(ctx context.Context) error {
	if ctx == nil {
		return boyan.PermanentError(boyan.CodeInvalidDerived, "context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return boyan.RetryableError(boyan.CodeTimeout, "session-memory operation canceled", err)
	}
	return nil
}
