package memory

import (
	"strings"
	"time"
)

// NewBoundary builds and validates an idempotent lifecycle export.
func NewBoundary(scope Scope, conversation ConversationRef, transitionID string, reason BoundaryReason, occurredAt time.Time) (Boundary, error) {
	exportID, err := BoundaryExportID(scope, conversation, transitionID)
	if err != nil {
		return Boundary{}, err
	}
	boundary := Boundary{
		SchemaVersion: SchemaVersionV1,
		ExportID:      exportID,
		Scope:         scope,
		Conversation:  conversation,
		TransitionID:  strings.TrimSpace(transitionID),
		Reason:        reason,
		OccurredAt:    occurredAt,
	}
	if err := boundary.Validate(); err != nil {
		return Boundary{}, err
	}
	return boundary, nil
}
