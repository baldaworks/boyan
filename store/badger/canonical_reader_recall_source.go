package badger

import (
	"encoding/json"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func recallCategory(kind memory.MemoryKind) memory.AtomCategory {
	if kind == memory.MemoryKindEvent {
		return memory.AtomCategoryEvent
	}
	return memory.AtomCategoryFact
}

func appendUnique(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

type canonicalTurnSourcePayload struct {
	ExportID       string                    `json:"export_id"`
	SessionID      string                    `json:"session_id"`
	AgentSessionID string                    `json:"agent_session_id"`
	SourceTurnID   string                    `json:"source_turn_id"`
	CompletedAt    time.Time                 `json:"completed_at"`
	TerminalStatus memory.TurnTerminalStatus `json:"terminal_status"`
}

func canonicalCompatibilityPayload(payload []byte, scope memory.Scope) (memory.CanonicalCompatibilityPayload, bool) {
	var compatibility memory.CanonicalCompatibilityPayload
	if err := json.Unmarshal(payload, &compatibility); err != nil {
		return memory.CanonicalCompatibilityPayload{}, false
	}
	if err := compatibility.Validate(scope); err != nil {
		return memory.CanonicalCompatibilityPayload{}, false
	}
	return compatibility, true
}

// canonicalSourceRef accepts both native canonical turn payloads and the
// validated v1 SourceRecord payload emitted by the migration adapter.
func canonicalSourceRef(payload []byte, scope memory.Scope, sourceID string) (memory.SourceRef, string, bool) {
	var source memory.SourceRecord
	if err := json.Unmarshal(payload, &source); err == nil && source.Ref.Scope == scope && source.Ref.SessionID != "" {
		return source.Ref, source.Ref.SessionID, true
	}
	var turn canonicalTurnSourcePayload
	if err := json.Unmarshal(payload, &turn); err != nil || turn.ExportID == "" || turn.SessionID == "" || turn.SourceTurnID == "" {
		return memory.SourceRef{}, "", false
	}
	ref := memory.SourceRef{Scope: scope, ExportID: turn.ExportID, SessionID: turn.SessionID, SourceTurnID: turn.SourceTurnID}
	if ref.Validate() != nil || sourceID == "" {
		return memory.SourceRef{}, "", false
	}
	return ref, turn.SessionID, true
}

func payloadScope() memory.Scope {
	return memory.Scope{Key: "internal:payload", Kind: memory.ScopeKindPersonal}
}
