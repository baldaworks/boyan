package memory

import (
	"context"
	"time"
)

// ScopeCheckpointKind identifies the lifecycle trigger that produced a
// checkpoint. All kinds use one exact scope key and are persisted separately.
type ScopeCheckpointKind string

const (
	ScopeCheckpointTurn     ScopeCheckpointKind = "turn"
	ScopeCheckpointToken    ScopeCheckpointKind = "token"
	ScopeCheckpointTime     ScopeCheckpointKind = "time"
	ScopeCheckpointBoundary ScopeCheckpointKind = "boundary"
)

// Validate verifies one checkpoint kind value.
func (k ScopeCheckpointKind) Validate() error {
	switch k {
	case ScopeCheckpointTurn, ScopeCheckpointToken, ScopeCheckpointTime, ScopeCheckpointBoundary:
		return nil
	default:
		return invalidDerived("unsupported scope checkpoint kind")
	}
}

// ScopeCheckpoint is the durable per-scope cursor used by synthesis and
// projection workers. It is intentionally independent from transport types.
type ScopeCheckpoint struct {
	SchemaVersion string              `json:"schema_version"`
	Scope         Scope               `json:"scope"`
	Kind          ScopeCheckpointKind `json:"kind"`
	CheckpointID  string              `json:"checkpoint_id"`
	ScopeVersion  uint64              `json:"scope_version"`
	ChangeSeq     uint64              `json:"change_seq"`
	TokenCount    uint64              `json:"token_count,omitempty"`
	OccurredAt    time.Time           `json:"occurred_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
}

// Validate verifies one durable projection checkpoint.
func (c ScopeCheckpoint) Validate() error {
	if c.SchemaVersion != ScopeCheckpointSchemaVersion {
		return invalidDerived("scope checkpoint schema version is invalid")
	}
	if err := c.Scope.Validate(); err != nil {
		return err
	}
	if err := c.Kind.Validate(); err != nil {
		return err
	}
	if !isCanonicalID(c.CheckpointID) || c.OccurredAt.IsZero() || c.UpdatedAt.IsZero() {
		return invalidDerived("scope checkpoint identity or timestamp is invalid")
	}
	if c.Kind != ScopeCheckpointToken && c.TokenCount != 0 {
		return invalidDerived("non-token checkpoint contains a token count")
	}
	return nil
}

// ScopeCheckpointStore persists the latest checkpoint for each exact scope
// and trigger kind. Implementations must make a checkpoint update durable
// before returning and reject backwards cursors.
type ScopeCheckpointStore interface {
	LoadScopeCheckpoint(ctx context.Context, scope Scope, kind ScopeCheckpointKind) (ScopeCheckpoint, bool, error)
	SaveScopeCheckpoint(ctx context.Context, checkpoint ScopeCheckpoint) error
}
