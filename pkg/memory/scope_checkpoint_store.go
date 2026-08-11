package memory

import "context"

// ScopeCheckpointStore persists the latest checkpoint for each exact scope
// and trigger kind. Implementations must make a checkpoint update durable
// before returning and reject backwards cursors.
type ScopeCheckpointStore interface {
	LoadScopeCheckpoint(ctx context.Context, scope Scope, kind ScopeCheckpointKind) (ScopeCheckpoint, bool, error)
	SaveScopeCheckpoint(ctx context.Context, checkpoint ScopeCheckpoint) error
}
