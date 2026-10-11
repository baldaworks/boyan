package memory

import (
	"strings"
)

// TurnSourceID derives the stable content-free source identifier for one turn
// export. It is safe to persist beside the source metadata.
func TurnSourceID(exportID string) string {
	return stableExportID("source", strings.TrimSpace(exportID))
}

// TurnMessageID derives the stable content-free message identifier for one
// role in a turn export. A turn has at most one message for each allowed role.
func TurnMessageID(exportID string, role MessageRole) string {
	return stableExportID("message", strings.TrimSpace(exportID), string(role))
}

// TurnToolMessageID derives a stable content-free identity for one typed tool
// response within a terminal turn.
func TurnToolMessageID(exportID, toolName, toolCallID string) string {
	return stableExportID(
		"tool-message",
		strings.TrimSpace(exportID),
		strings.TrimSpace(toolName),
		strings.TrimSpace(toolCallID),
	)
}

// TurnExportID derives a stable idempotency key for one source turn.
func TurnExportID(scope Scope, conversation ConversationRef, sourceTurnID string) (string, error) {
	if err := scope.Validate(); err != nil {
		return "", err
	}

	if err := conversation.Validate(); err != nil {
		return "", err
	}

	sourceTurnID = strings.TrimSpace(sourceTurnID)
	if sourceTurnID == "" {
		return "", PermanentError(CodeInvalidConversation, "source turn id is required", nil)
	}

	return stableExportID("turn", scope.Key, conversation.ConversationID, sourceTurnID), nil
}

// BoundaryExportID derives a stable idempotency key for one lifecycle transition.
func BoundaryExportID(scope Scope, conversation ConversationRef, transitionID string) (string, error) {
	if err := scope.Validate(); err != nil {
		return "", err
	}

	if err := conversation.Validate(); err != nil {
		return "", err
	}

	transitionID = strings.TrimSpace(transitionID)
	if transitionID == "" {
		return "", PermanentError(CodeInvalidConversation, "transition id is required", nil)
	}

	return stableExportID("boundary", scope.Key, conversation.ConversationID, transitionID), nil
}
