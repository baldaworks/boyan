package memory

import (
	"fmt"
	"strings"
)

func validateMessage(message Message, exportID string, role MessageRole) error {
	if message.Role != role {
		return PermanentError(CodePermanent, fmt.Sprintf("turn message role must be %s", role), nil)
	}
	if strings.TrimSpace(message.Text) == "" {
		return PermanentError(CodePermanent, fmt.Sprintf("%s message text is required", role), nil)
	}
	if message.MessageID != "" && message.MessageID != TurnMessageID(exportID, role) {
		return PermanentError(CodePermanent, fmt.Sprintf("%s message id does not match turn identity", role), nil)
	}
	if message.ToolName != "" || message.ToolCallID != "" {
		return PermanentError(CodePermanent, "non-tool message contains tool identity", nil)
	}
	return nil
}

func validateToolMessage(message Message, exportID string) error {
	if message.Role != MessageRoleTool || strings.TrimSpace(message.Text) == "" || !isCanonicalID(message.ToolName) || !isCanonicalID(message.ToolCallID) {
		return PermanentError(CodePermanent, "turn tool message is invalid", nil)
	}
	if message.MessageID != TurnToolMessageID(exportID, message.ToolName, message.ToolCallID) {
		return PermanentError(CodePermanent, "tool message id does not match turn identity", nil)
	}
	return nil
}
