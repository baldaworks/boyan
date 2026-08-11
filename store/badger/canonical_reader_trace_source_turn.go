package badger

import memory "github.com/baldaworks/boyan/pkg/memory"

func nativeTraceTurn(
	scope memory.Scope,
	sourceID string,
	payload canonicalTurnSourcePayload,
	messages []memory.Message,
) (memory.Turn, error) {
	if len(messages) == 0 {
		return memory.Turn{}, memory.PermanentError(memory.CodeInvalidDerived, "canonical trace source has no messages", nil)
	}
	turn := memory.Turn{
		SchemaVersion:  memory.SchemaVersionV1,
		ExportID:       payload.ExportID,
		Scope:          scope,
		Conversation:   memory.ConversationRef{ConversationID: payload.ConversationID, AgentConversationID: payload.AgentConversationID},
		SourceTurnID:   payload.SourceTurnID,
		SourceID:       sourceID,
		CompletedAt:    payload.CompletedAt,
		TerminalStatus: payload.TerminalStatus,
		Messages:       messages,
	}
	if err := turn.Validate(); err != nil {
		return memory.Turn{}, memory.PermanentError(memory.CodeStoreFailure, "canonical trace source turn is invalid", err)
	}
	return turn, nil
}
