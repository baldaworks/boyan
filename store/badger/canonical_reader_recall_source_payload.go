package badger

import (
	"encoding/json"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

type canonicalTurnSourcePayload struct {
	ExportID            string                    `json:"export_id"`
	ConversationID      string                    `json:"conversation_id"`
	AgentConversationID string                    `json:"agent_conversation_id"`
	SourceTurnID        string                    `json:"source_turn_id"`
	CompletedAt         time.Time                 `json:"completed_at"`
	TerminalStatus      memory.TurnTerminalStatus `json:"terminal_status"`
}

// canonicalSourceRef accepts both native canonical turn payloads and the
// validated v1 SourceRecord payload emitted by the migration adapter.
func canonicalSourceRef(payload []byte, scope memory.Scope, sourceID string) (memory.SourceRef, string, bool) {
	var source memory.SourceRecord
	if err := json.Unmarshal(payload, &source); err == nil && source.Ref.Scope == scope &&
		source.Ref.ConversationID != "" {
		return source.Ref, source.Ref.ConversationID, true
	}

	var turn canonicalTurnSourcePayload
	if err := json.Unmarshal(payload, &turn); err != nil || turn.ExportID == "" || turn.ConversationID == "" ||
		turn.SourceTurnID == "" {
		return memory.SourceRef{}, "", false
	}

	ref := memory.SourceRef{
		Scope:          scope,
		ExportID:       turn.ExportID,
		ConversationID: turn.ConversationID,
		SourceTurnID:   turn.SourceTurnID,
	}
	if ref.Validate() != nil || sourceID == "" {
		return memory.SourceRef{}, "", false
	}

	return ref, turn.ConversationID, true
}

func payloadScope() memory.Scope {
	return memory.Scope{Key: "internal:payload", Kind: memory.ScopeKindPersonal}
}
