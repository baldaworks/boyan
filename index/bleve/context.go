package bleve

import (
	"context"

	"github.com/baldaworks/boyan"
)

func sessionMemoryContextError(ctx context.Context) error {
	if ctx == nil {
		return boyan.PermanentError(boyan.CodeInvalidDerived, "context is required", nil)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
