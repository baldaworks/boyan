package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// CanonicalTurnFingerprintVersion fixes the authoritative input representation.
const CanonicalTurnFingerprintVersion uint32 = 1

// NormalizeCanonicalTurn validates an export and returns an independent canonical
// representation. Delivery producer, sequence and caller fingerprints are separate.
func NormalizeCanonicalTurn(turn Turn) (Turn, error) {
	if err := turn.Validate(); err != nil {
		return Turn{}, err
	}

	turn.Messages = append([]Message(nil), turn.Messages...)
	turn.SourceID = TurnSourceID(turn.ExportID)

	turn.CompletedAt = turn.CompletedAt.UTC()
	if turn.TerminalStatus == "" {
		turn.TerminalStatus = TurnTerminalStatusSuccess
	}

	for index := range turn.Messages {
		if turn.Messages[index].MessageID == "" {
			turn.Messages[index].MessageID = TurnMessageID(turn.ExportID, turn.Messages[index].Role)
		}
	}

	return turn, nil
}

type canonicalTurnIdentityV1 struct {
	Version                uint32                           `json:"version"`
	SchemaVersion          string                           `json:"schema_version"`
	ExportID               string                           `json:"export_id"`
	ScopeKey               string                           `json:"scope_key"`
	ScopeKind              ScopeKind                        `json:"scope_kind"`
	ConversationID         string                           `json:"conversation_id"`
	LineageID              string                           `json:"lineage_id"`
	PreviousConversationID string                           `json:"previous_conversation_id"`
	AgentConversationID    string                           `json:"agent_conversation_id"`
	SourceTurnID           string                           `json:"source_turn_id"`
	SourceID               string                           `json:"source_id"`
	CompletedAt            string                           `json:"completed_at"`
	TerminalStatus         TurnTerminalStatus               `json:"terminal_status"`
	Messages               []canonicalTurnIdentityMessageV1 `json:"messages"`
}

type canonicalTurnIdentityMessageV1 struct {
	MessageID  string      `json:"message_id"`
	Role       MessageRole `json:"role"`
	Text       string      `json:"text"`
	ToolName   string      `json:"tool_name"`
	ToolCallID string      `json:"tool_call_id"`
}

// CanonicalTurnFingerprint binds all normalized authoritative fields, including
// message order. Its versioned representation does not depend on Turn's layout.
func CanonicalTurnFingerprint(turn Turn) (string, error) {
	normalized, err := NormalizeCanonicalTurn(turn)
	if err != nil {
		return "", err
	}

	encoded, err := json.Marshal(canonicalTurnIdentityWire(normalized))
	if err != nil {
		return "", PermanentError(CodePermanent, "encode canonical turn identity", err)
	}

	sum := sha256.Sum256(encoded)

	return hex.EncodeToString(sum[:]), nil
}

func canonicalTurnIdentityWire(turn Turn) canonicalTurnIdentityV1 {
	wire := canonicalTurnIdentityV1{
		Version: CanonicalTurnFingerprintVersion, SchemaVersion: turn.SchemaVersion,
		ExportID: turn.ExportID, ScopeKey: turn.Scope.Key, ScopeKind: turn.Scope.Kind,
		ConversationID: turn.Conversation.ConversationID, AgentConversationID: turn.Conversation.AgentConversationID,
		LineageID: turn.Conversation.LineageID, PreviousConversationID: turn.Conversation.PreviousConversationID,
		SourceTurnID: turn.SourceTurnID, SourceID: turn.SourceID,
		CompletedAt: turn.CompletedAt.Format(time.RFC3339Nano), TerminalStatus: turn.TerminalStatus,
		Messages: make([]canonicalTurnIdentityMessageV1, len(turn.Messages)),
	}
	for index, message := range turn.Messages {
		wire.Messages[index] = canonicalTurnIdentityMessageV1{MessageID: message.MessageID,
			Role: message.Role, Text: message.Text, ToolName: message.ToolName, ToolCallID: message.ToolCallID}
	}

	return wire
}
