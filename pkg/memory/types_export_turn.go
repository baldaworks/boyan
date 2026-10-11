package memory

import (
	"strings"
	"time"
)

// NewTurn builds and validates an idempotent completed-turn export.
func NewTurn(
	scope Scope,
	conversation ConversationRef,
	sourceTurnID string,
	completedAt time.Time,
	userText, assistantText string,
) (Turn, error) {
	return NewTerminalTurn(
		scope,
		conversation,
		sourceTurnID,
		completedAt,
		userText,
		assistantText,
		TurnTerminalStatusSuccess,
	)
}

// NewTerminalTurn builds one idempotent terminal export. A user-only export is
// valid solely for a failed or interrupted provider turn.
func NewTerminalTurn(
	scope Scope,
	conversation ConversationRef,
	sourceTurnID string,
	completedAt time.Time,
	userText, assistantText string,
	terminalStatus TurnTerminalStatus,
) (Turn, error) {
	return NewTerminalTurnWithTools(
		scope,
		conversation,
		sourceTurnID,
		completedAt,
		userText,
		assistantText,
		nil,
		terminalStatus,
	)
}

// NewTerminalTurnWithTools builds a terminal export with already allowlisted,
// typed tool evidence. Tool identities are application-derived; callers cannot
// supply durable message IDs.
func NewTerminalTurnWithTools(
	scope Scope,
	conversation ConversationRef,
	sourceTurnID string,
	completedAt time.Time,
	userText, assistantText string,
	tools []Message,
	terminalStatus TurnTerminalStatus,
) (Turn, error) {
	exportID, err := TurnExportID(scope, conversation, sourceTurnID)
	if err != nil {
		return Turn{}, err
	}

	messages := []Message{
		{
			MessageID: TurnMessageID(exportID, MessageRoleUser),
			Role:      MessageRoleUser,
			Text:      strings.TrimSpace(userText),
		},
	}
	if assistantText = strings.TrimSpace(assistantText); assistantText != "" {
		messages = append(
			messages,
			Message{
				MessageID: TurnMessageID(exportID, MessageRoleAssistant),
				Role:      MessageRoleAssistant,
				Text:      assistantText,
			},
		)
	}

	for _, tool := range tools {
		tool.Role = MessageRoleTool
		tool.MessageID = TurnToolMessageID(exportID, tool.ToolName, tool.ToolCallID)
		tool.Text = strings.TrimSpace(tool.Text)
		tool.ToolName = strings.TrimSpace(tool.ToolName)
		tool.ToolCallID = strings.TrimSpace(tool.ToolCallID)
		messages = append(messages, tool)
	}

	turn := Turn{
		SchemaVersion:  SchemaVersionV1,
		ExportID:       exportID,
		Scope:          scope,
		Conversation:   conversation,
		SourceTurnID:   strings.TrimSpace(sourceTurnID),
		SourceID:       TurnSourceID(exportID),
		CompletedAt:    completedAt,
		TerminalStatus: terminalStatus,
		Messages:       messages,
	}
	if err := turn.Validate(); err != nil {
		return Turn{}, err
	}

	return turn, nil
}
