package semanticprojection

import (
	"context"
	semanticcore "github.com/baldaworks/boyan/internal/semantic"
)

// Legacy observation metadata must be rebuilt before readiness can certify recall.
func (manager *Manager) observedMetadataReady(ctx context.Context, view semanticcore.View) bool {
	ready, err := manager.store.ObservedMetadataReady(ctx, view)

	return err == nil && ready
}
