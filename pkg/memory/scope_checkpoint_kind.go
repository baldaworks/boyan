package memory

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
