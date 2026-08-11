package memory

import "time"

const maxTurnToolMessages = 16

// MessageRole identifies one eligible conversational message role.
type MessageRole string

const (
	// MessageRoleUser identifies normalized user-visible input.
	MessageRoleUser MessageRole = "user"
	// MessageRoleAssistant identifies final visible assistant output.
	MessageRoleAssistant MessageRole = "assistant"
	// MessageRoleTool identifies allowlisted typed tool evidence.
	MessageRoleTool MessageRole = "tool"
)

// TurnTerminalStatus identifies how the provider turn ended. Empty is
// accepted only for legacy successful exports and normalizes to success.
type TurnTerminalStatus string

const (
	TurnTerminalStatusSuccess     TurnTerminalStatus = "success"
	TurnTerminalStatusFailed      TurnTerminalStatus = "failed"
	TurnTerminalStatusInterrupted TurnTerminalStatus = "interrupted"
)

// Message is one text-only conversational message in a completed turn.
type Message struct {
	MessageID  string      `json:"message_id,omitempty"`
	Role       MessageRole `json:"role"`
	Text       string      `json:"text"`
	ToolName   string      `json:"tool_name,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
}

// Turn is an idempotent terminal-turn export. It always contains normalized
// user input and may omit assistant text when the provider terminally fails or
// is interrupted before producing a visible response.
type Turn struct {
	SchemaVersion  string             `json:"schema_version"`
	ExportID       string             `json:"export_id"`
	Scope          Scope              `json:"scope"`
	Conversation   ConversationRef    `json:"conversation"`
	SourceTurnID   string             `json:"source_turn_id"`
	SourceID       string             `json:"source_id,omitempty"`
	CompletedAt    time.Time          `json:"completed_at"`
	TerminalStatus TurnTerminalStatus `json:"terminal_status,omitempty"`
	Messages       []Message          `json:"messages"`
}
