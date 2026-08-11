package memory

import "time"

// BoundaryReason identifies a conversation lifecycle transition.
type BoundaryReason string

const (
	// BoundaryReasonReset marks a history reset within an existing locator.
	BoundaryReasonReset BoundaryReason = "reset"
	// BoundaryReasonClose marks an explicitly closed conversation.
	BoundaryReasonClose BoundaryReason = "close"
	// BoundaryReasonRotation marks replacement by a new conversation lineage.
	BoundaryReasonRotation BoundaryReason = "rotation"
	// BoundaryReasonShutdown marks bounded application shutdown extraction.
	BoundaryReasonShutdown BoundaryReason = "shutdown"
)

// Boundary is an idempotent conversation lifecycle export.
type Boundary struct {
	SchemaVersion string          `json:"schema_version"`
	ExportID      string          `json:"export_id"`
	Scope         Scope           `json:"scope"`
	Conversation  ConversationRef `json:"conversation"`
	TransitionID  string          `json:"transition_id"`
	Reason        BoundaryReason  `json:"reason"`
	OccurredAt    time.Time       `json:"occurred_at"`
}
