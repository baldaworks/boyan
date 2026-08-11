package memory

// ScopeKind classifies a locator without changing its isolation boundary.
type ScopeKind string

const (
	// ScopeKindPersonal is a direct or private conversation scope.
	ScopeKindPersonal ScopeKind = "personal"
	// ScopeKindGroup is a shared group, channel, or stream scope. A topic or
	// thread may still have this kind when its parent audience is shared.
	ScopeKindGroup ScopeKind = "group"
)

// Scope identifies the exact locator partition used for export and recall.
type Scope struct {
	Key  string    `json:"key"`
	Kind ScopeKind `json:"kind"`
}

// ConversationRef identifies the standalone conversation and its runtime lineage.
type ConversationRef struct {
	ConversationID         string `json:"conversation_id"`
	AgentConversationID    string `json:"agent_conversation_id"`
	LineageID              string `json:"lineage_id,omitempty"`
	PreviousConversationID string `json:"previous_conversation_id,omitempty"`
}
