package memory

import (
	"time"
)

const (
	// RecallSchemaVersionV1 identifies the bounded canonical recall contract.
	RecallSchemaVersionV1 = "boyan-recall/v1"
	// MaxRecallCandidates bounds projection candidates before canonical hydration.
	MaxRecallCandidates = 256
	// MaxRecallEvidence bounds compact evidence returned with one recall hit.
	MaxRecallEvidence = 16
)

// RecallRequest is the storage-neutral, exact-scope retrieval request.
type RecallRequest struct {
	SchemaVersion     string        `json:"schema_version"`
	Scope             Scope         `json:"scope"`
	Query             string        `json:"query"`
	Limit             int           `json:"limit"`
	Kind              *MemoryKind   `json:"kind,omitempty"`
	Category          *AtomCategory `json:"category,omitempty"`
	AsOf              *time.Time    `json:"as_of,omitempty"`
	SourceID          string        `json:"source_id,omitempty"`
	ConversationID    string        `json:"conversation_id,omitempty"`
	MemoryKey         MemoryKey     `json:"memory_key,omitempty"`
	MinScopeChangeSeq uint64        `json:"min_scope_change_seq,omitempty"`
}
