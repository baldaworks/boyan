package semanticprojection //nolint:testpackage // Shared timing boundary used by the actual SQLite successor.

import (
	"context"
	"github.com/baldaworks/boyan/pkg/memory"
	"time"
)

type delayedRecallReader struct{ memory.RecallCanonicalReader }

func (reader delayedRecallReader) CurrentScopeChangeSeq(ctx context.Context, scope memory.Scope) (uint64, error) {
	time.Sleep(2 * time.Second)

	return reader.RecallCanonicalReader.CurrentScopeChangeSeq(ctx, scope)
}

type queryProviderFunc func(context.Context, []string) ([][]float32, error)

func (f queryProviderFunc) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	return f(ctx, inputs)
}
