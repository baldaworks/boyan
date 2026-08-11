package memory

import "context"

// ProjectionRetentionFloorReader reports the minimum watermark that remains
// safe for incremental replay after retention removed older change records. A
// floor greater than a generation's watermark requires a complete rebuild
// from canonical active state.
// Implementations may omit this optional port when change records are retained
// indefinitely.
type ProjectionRetentionFloorReader interface {
	LoadProjectionRetentionFloor(ctx context.Context, scope Scope, projectionID string) (uint64, error)
}

// ProjectionRetentionFloorWriter is the maintenance-side port for advancing
// the replay floor after an explicitly verified retention operation.
type ProjectionRetentionFloorWriter interface {
	ProjectionRetentionFloorReader
	SetProjectionRetentionFloor(ctx context.Context, scope Scope, projectionID string, floor uint64) error
}
