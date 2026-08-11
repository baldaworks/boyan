package bleve

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (a *BleveCanonicalApplier) Commit(ctx context.Context, _ memory.Scope, generationID string) error {
	a.mu.Lock()
	generation := a.generations[generationID]
	a.mu.Unlock()
	if generation == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "bleve generation is not initialized", nil)
	}
	return generation.Commit(ctx)
}
