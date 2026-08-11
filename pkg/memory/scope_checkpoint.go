package memory

import (
	"time"
)

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
