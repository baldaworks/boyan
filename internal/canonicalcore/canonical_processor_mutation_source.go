package canonicalcore

import (
	"encoding/json"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func (p *CanonicalTurnProcessor) appendCanonicalTurnSource(mutation *CanonicalMutation, turn Turn) (string, error) {
	sourceID := turn.SourceID
	if !isCanonicalID(sourceID) {
		return "", invalidDerived("canonical turn source identity is invalid")
	}

	sourcePayload, err := json.Marshal(canonicalTurnSourcePayload{
		ExportID:            turn.ExportID,
		ConversationID:      turn.Conversation.ConversationID,
		AgentConversationID: turn.Conversation.AgentConversationID,
		SourceTurnID:        turn.SourceTurnID,
		CompletedAt:         turn.CompletedAt,
		TerminalStatus:      normalizedTerminalStatus(turn),
	})
	if err != nil {
		return "", PermanentError(CodeInvalidDerived, "encode canonical source payload", err)
	}
	sourcePayloadRecord, sourceRef, err := p.payload(canonicalPayloadID("source", sourceID), sourcePayload)
	if err != nil {
		return "", err
	}
	mutation.Sources = []CanonicalSourceRecord{{
		SourceID:    sourceID,
		Scope:       turn.Scope,
		Sensitivity: SensitivityStandard,
		Retention:   RetentionClassStandard,
		Payload:     sourceRef,
	}}
	mutation.Payloads = append(mutation.Payloads, sourcePayloadRecord)
	return sourceID, nil
}
