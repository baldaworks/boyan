package badger

import (
	"context"

	"github.com/baldaworks/boyan"
)

func canonicalContextError(ctx context.Context) error {
	if ctx == nil {
		return boyan.PermanentError(boyan.CodeInvalidDerived, "context is required", nil)
	}
	if err := ctx.Err(); err != nil {
		return boyan.RetryableError(boyan.CodeTimeout, "canonical store operation canceled", err)
	}
	return nil
}
