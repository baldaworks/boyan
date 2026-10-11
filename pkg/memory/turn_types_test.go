package memory_test

import memory "github.com/baldaworks/boyan/pkg/memory"

import (
	"testing"
	"time"
)

func TestNewTurnBuildsStableTextOnlyExport(t *testing.T) {
	t.Parallel()

	scope, conversation := testIdentity()
	completedAt := time.Date(2026, time.August, 3, 4, 5, 6, 0, time.UTC)

	first, err := memory.NewTurn(scope, conversation, "turn-42", completedAt, " hello ", " world ")
	if err != nil {
		t.Fatalf("NewTurn() error = %v", err)
	}

	second, err := memory.NewTurn(scope, conversation, "turn-42", completedAt, "hello", "world")
	if err != nil {
		t.Fatalf("NewTurn() second error = %v", err)
	}

	if first.ExportID != second.ExportID {
		t.Fatalf("ExportID = %q, want stable %q", second.ExportID, first.ExportID)
	}

	if first.Messages[0] != (memory.Message{
		MessageID: memory.TurnMessageID(first.ExportID, memory.MessageRoleUser),
		Role:      memory.MessageRoleUser,
		Text:      "hello",
	}) {
		t.Fatalf("user message = %+v", first.Messages[0])
	}

	if first.Messages[1] != (memory.Message{
		MessageID: memory.TurnMessageID(first.ExportID, memory.MessageRoleAssistant),
		Role:      memory.MessageRoleAssistant,
		Text:      "world",
	}) {
		t.Fatalf("assistant message = %+v", first.Messages[1])
	}

	if err := first.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if first.SourceID != memory.TurnSourceID(first.ExportID) {
		t.Fatalf("source id = %q, want deterministic id", first.SourceID)
	}
}

func TestTurnValidateRejectsNonTextShapeAndChangedIdentity(t *testing.T) {
	t.Parallel()

	scope, conversation := testIdentity()

	turn, err := memory.NewTurn(scope, conversation, "turn-1", time.Now().UTC(), "user", "assistant")
	if err != nil {
		t.Fatalf("NewTurn() error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*memory.Turn)
	}{
		{
			name: "extra message",
			mutate: func(turn *memory.Turn) {
				turn.Messages = append(turn.Messages, memory.Message{Role: memory.MessageRoleTool, Text: "tool"})
			},
		},
		{
			name:   "wrong role",
			mutate: func(turn *memory.Turn) { turn.Messages[1].Role = memory.MessageRoleUser },
		},
		{name: "empty user", mutate: func(turn *memory.Turn) { turn.Messages[0].Text = "  " }},
		{
			name:   "changed message id",
			mutate: func(turn *memory.Turn) { turn.Messages[0].MessageID = "other-message" },
		},
		{
			name:   "changed source id",
			mutate: func(turn *memory.Turn) { turn.SourceID = "other-source" },
		},
		{
			name:   "changed source identity",
			mutate: func(turn *memory.Turn) { turn.SourceTurnID = "turn-2" },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := turn
			candidate.Messages = append([]memory.Message(nil), turn.Messages...)
			tt.mutate(&candidate)

			if err := candidate.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
		})
	}
}

func TestNewTerminalTurnAllowsUserOnlyFailureButNotSuccess(t *testing.T) {
	scope, conversation := testIdentity()

	failed, err := memory.NewTerminalTurn(
		scope,
		conversation,
		"turn-failed",
		time.Now().UTC(),
		"user",
		"",
		memory.TurnTerminalStatusFailed,
	)
	if err != nil || len(failed.Messages) != 1 ||
		failed.TerminalStatus != memory.TurnTerminalStatusFailed {
		t.Fatalf("NewTerminalTurn(failed) = %+v, %v", failed, err)
	}

	if _, err := memory.NewTerminalTurn(
		scope,
		conversation,
		"turn-success",
		time.Now().UTC(),
		"user",
		"",
		memory.TurnTerminalStatusSuccess,
	); err == nil {
		t.Fatal("NewTerminalTurn(success without assistant) error = nil")
	}
}

func TestNewTerminalTurnWithToolsBuildsStableTypedMessages(t *testing.T) {
	t.Parallel()

	scope := memory.Scope{Key: "telegram:tools", Kind: memory.ScopeKindPersonal}
	conversation := memory.ConversationRef{ConversationID: "conversation-tools", AgentConversationID: "agent-tools"}

	turn, err := memory.NewTerminalTurnWithTools(
		scope,
		conversation,
		"turn-tools",
		time.Now().UTC(),
		"question",
		"answer",
		[]memory.Message{{ToolName: "calendar.lookup", ToolCallID: "call-1", Text: "2026-08-06"}},
		memory.TurnTerminalStatusSuccess,
	)
	if err != nil {
		t.Fatalf("NewTerminalTurnWithTools() error = %v", err)
	}

	if len(turn.Messages) != 3 {
		t.Fatalf("messages = %#v", turn.Messages)
	}

	tool := turn.Messages[2]
	if tool.Role != memory.MessageRoleTool ||
		tool.MessageID != memory.TurnToolMessageID(turn.ExportID, "calendar.lookup", "call-1") ||
		tool.Text != "2026-08-06" {
		t.Fatalf("tool message = %#v", tool)
	}
}
