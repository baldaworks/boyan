package bleve

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (a *BleveCanonicalApplier) Activate(ctx context.Context, scope memory.Scope, generationID string) error {
	a.mu.Lock()
	generation := a.generations[generationID]
	delete(a.generations, generationID)
	a.mu.Unlock()
	if generation == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "bleve generation is not initialized", nil)
	}
	if err := generation.Close(); err != nil {
		return err
	}
	return a.projection.ActivateGenerationForScope(ctx, scope, generationID)
}
