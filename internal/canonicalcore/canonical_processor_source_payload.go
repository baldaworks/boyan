package canonicalcore

import (
	"strings"
	"time"

	. "github.com/baldaworks/boyan/pkg/memory"
)

type canonicalTurnSourcePayload struct {
	ExportID            string             `json:"export_id"`
	ConversationID      string             `json:"conversation_id"`
	AgentConversationID string             `json:"agent_conversation_id"`
	SourceTurnID        string             `json:"source_turn_id"`
	CompletedAt         time.Time          `json:"completed_at"`
	TerminalStatus      TurnTerminalStatus `json:"terminal_status"`
}

func normalizedTerminalStatus(turn Turn) TurnTerminalStatus {
	if strings.TrimSpace(string(turn.TerminalStatus)) == "" {
		return TurnTerminalStatusSuccess
	}
	return turn.TerminalStatus
}
