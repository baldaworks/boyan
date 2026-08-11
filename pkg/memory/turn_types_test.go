package memory

import (
	"testing"
	"time"
)

func TestNewTurnBuildsStableTextOnlyExport(t *testing.T) {
	t.Parallel()

	scope, conversation := testIdentity()
	completedAt := time.Date(2026, time.August, 3, 4, 5, 6, 0, time.UTC)
	first, err := NewTurn(scope, conversation, "turn-42", completedAt, " hello ", " world ")
	if err != nil {
		t.Fatalf("NewTurn() error = %v", err)
	}
	second, err := NewTurn(scope, conversation, "turn-42", completedAt, "hello", "world")
	if err != nil {
		t.Fatalf("NewTurn() second error = %v", err)
	}
	if first.ExportID != second.ExportID {
		t.Fatalf("ExportID = %q, want stable %q", second.ExportID, first.ExportID)
	}
	if first.Messages[0] != (Message{MessageID: TurnMessageID(first.ExportID, MessageRoleUser), Role: MessageRoleUser, Text: "hello"}) {
		t.Fatalf("user message = %+v", first.Messages[0])
	}
	if first.Messages[1] != (Message{MessageID: TurnMessageID(first.ExportID, MessageRoleAssistant), Role: MessageRoleAssistant, Text: "world"}) {
		t.Fatalf("assistant message = %+v", first.Messages[1])
	}
	if err := first.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if first.SourceID != TurnSourceID(first.ExportID) {
		t.Fatalf("source id = %q, want deterministic id", first.SourceID)
	}
}

func TestTurnValidateRejectsNonTextShapeAndChangedIdentity(t *testing.T) {
	t.Parallel()

	scope, conversation := testIdentity()
	turn, err := NewTurn(scope, conversation, "turn-1", time.Now().UTC(), "user", "assistant")
	if err != nil {
		t.Fatalf("NewTurn() error = %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*Turn)
	}{
		{name: "extra message", mutate: func(turn *Turn) { turn.Messages = append(turn.Messages, Message{Role: MessageRoleTool, Text: "tool"}) }},
		{name: "wrong role", mutate: func(turn *Turn) { turn.Messages[1].Role = MessageRoleUser }},
		{name: "empty user", mutate: func(turn *Turn) { turn.Messages[0].Text = "  " }},
		{name: "changed message id", mutate: func(turn *Turn) { turn.Messages[0].MessageID = "other-message" }},
		{name: "changed source id", mutate: func(turn *Turn) { turn.SourceID = "other-source" }},
		{name: "changed source identity", mutate: func(turn *Turn) { turn.SourceTurnID = "turn-2" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := turn
			candidate.Messages = append([]Message(nil), turn.Messages...)
			tt.mutate(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
		})
	}
}

func TestNewTerminalTurnAllowsUserOnlyFailureButNotSuccess(t *testing.T) {
	scope, conversation := testIdentity()
	failed, err := NewTerminalTurn(scope, conversation, "turn-failed", time.Now().UTC(), "user", "", TurnTerminalStatusFailed)
	if err != nil || len(failed.Messages) != 1 || failed.TerminalStatus != TurnTerminalStatusFailed {
		t.Fatalf("NewTerminalTurn(failed) = %+v, %v", failed, err)
	}
	if _, err := NewTerminalTurn(scope, conversation, "turn-success", time.Now().UTC(), "user", "", TurnTerminalStatusSuccess); err == nil {
		t.Fatal("NewTerminalTurn(success without assistant) error = nil")
	}
}

func TestNewTerminalTurnWithToolsBuildsStableTypedMessages(t *testing.T) {
	t.Parallel()
	scope := Scope{Key: "telegram:tools", Kind: ScopeKindPersonal}
	conversation := ConversationRef{ConversationID: "conversation-tools", AgentConversationID: "agent-tools"}
	turn, err := NewTerminalTurnWithTools(scope, conversation, "turn-tools", time.Now().UTC(), "question", "answer", []Message{{ToolName: "calendar.lookup", ToolCallID: "call-1", Text: "2026-08-06"}}, TurnTerminalStatusSuccess)
	if err != nil {
		t.Fatalf("NewTerminalTurnWithTools() error = %v", err)
	}
	if len(turn.Messages) != 3 {
		t.Fatalf("messages = %#v", turn.Messages)
	}
	tool := turn.Messages[2]
	if tool.Role != MessageRoleTool || tool.MessageID != TurnToolMessageID(turn.ExportID, "calendar.lookup", "call-1") || tool.Text != "2026-08-06" {
		t.Fatalf("tool message = %#v", tool)
	}
}
