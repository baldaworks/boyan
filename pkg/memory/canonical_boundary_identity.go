package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

// CanonicalBoundaryFingerprintVersion fixes the complete authoritative wire.
const CanonicalBoundaryFingerprintVersion uint32 = 1

// NormalizeCanonicalBoundary applies existing transition normalization and
// compares occurrence timestamps by UTC instant without monotonic clock data.
func NormalizeCanonicalBoundary(boundary Boundary) (Boundary, error) {
	if err := boundary.Validate(); err != nil {
		return Boundary{}, err
	}

	boundary.TransitionID = strings.TrimSpace(boundary.TransitionID)
	boundary.OccurredAt = boundary.OccurredAt.Round(0).UTC()

	return boundary, nil
}

type canonicalBoundaryIdentityV1 struct {
	Version                uint32         `json:"version"`
	SchemaVersion          string         `json:"schema_version"`
	ExportID               string         `json:"export_id"`
	ScopeKey               string         `json:"scope_key"`
	ScopeKind              ScopeKind      `json:"scope_kind"`
	ConversationID         string         `json:"conversation_id"`
	AgentConversationID    string         `json:"agent_conversation_id"`
	LineageID              string         `json:"lineage_id"`
	PreviousConversationID string         `json:"previous_conversation_id"`
	TransitionID           string         `json:"transition_id"`
	Reason                 BoundaryReason `json:"reason"`
	OccurredAt             string         `json:"occurred_at"`
}

// CanonicalBoundaryFingerprint binds every normalized Boundary field while
// remaining independent of transport deliveries and derivation versions.
func CanonicalBoundaryFingerprint(boundary Boundary) (string, error) {
	normalized, err := NormalizeCanonicalBoundary(boundary)
	if err != nil {
		return "", err
	}

	wire := canonicalBoundaryIdentityV1{
		Version: CanonicalBoundaryFingerprintVersion, SchemaVersion: normalized.SchemaVersion,
		ExportID: normalized.ExportID, ScopeKey: normalized.Scope.Key, ScopeKind: normalized.Scope.Kind,
		ConversationID:         normalized.Conversation.ConversationID,
		AgentConversationID:    normalized.Conversation.AgentConversationID,
		LineageID:              normalized.Conversation.LineageID,
		PreviousConversationID: normalized.Conversation.PreviousConversationID,
		TransitionID:           normalized.TransitionID, Reason: normalized.Reason,
		OccurredAt: normalized.OccurredAt.Format(time.RFC3339Nano),
	}

	raw, err := json.Marshal(wire)
	if err != nil {
		return "", PermanentError(CodePermanent, "encode canonical boundary identity", nil)
	}

	sum := sha256.Sum256(raw)

	return hex.EncodeToString(sum[:]), nil
}
