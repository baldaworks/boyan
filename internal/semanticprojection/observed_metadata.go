package semanticprojection

import (
	"context"

	semanticstore "github.com/baldaworks/boyan/index/semantic"
)

// Legacy vectors can be reused, but their missing observation metadata must be
// rebuilt before a same-view shortcut or readiness claim can certify recall.
func (manager *Manager) observedMetadataReady(ctx context.Context, view semanticstore.View) bool {
	snapshot, found, err := manager.store.Snapshot(ctx, view)
	if err != nil || !found {
		return false
	}

	ready, err := snapshotHasObservedMetadata(ctx, snapshot)

	return err == nil && ready
}

func snapshotHasObservedMetadata(ctx context.Context, snapshot *semanticstore.Snapshot) (bool, error) {
	for _, chunk := range snapshot.Chunks() {
		if err := ctx.Err(); err != nil {
			return false, err
		}

		if chunk.ObservedAt == nil || chunk.ObservedAt.IsZero() {
			return false, nil
		}
	}

	return true, ctx.Err()
}
