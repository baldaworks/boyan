package memory

import "time"

// RecallScore explains the bounded ranking components exposed to callers.
type RecallScore struct {
	Lexical    float64 `json:"lexical"`
	Freshness  float64 `json:"freshness"`
	ExactScope float64 `json:"exact_scope"`
	Total      float64 `json:"total"`
}

// RecallReference is an explicitly untrusted, compact recall result.
type RecallReference struct {
	SchemaVersion  string         `json:"schema_version"`
	Trust          ReferenceTrust `json:"trust"`
	Scope          Scope          `json:"scope"`
	ItemID         string         `json:"item_id"`
	RevisionID     string         `json:"revision_id"`
	Revision       uint64         `json:"revision"`
	Kind           MemoryKind     `json:"kind"`
	State          RevisionState  `json:"state"`
	Category       *AtomCategory  `json:"category,omitempty"`
	MemoryKey      MemoryKey      `json:"memory_key,omitempty"`
	Text           string         `json:"text"`
	CreatedAt      time.Time      `json:"created_at"`
	Score          float64        `json:"score"`
	Explain        RecallScore    `json:"explain"`
	Evidence       []EvidenceRef  `json:"evidence,omitempty"`
	ScopeChangeSeq uint64         `json:"scope_change_seq"`
}

// RecallResponse contains only bounded, hydrated untrusted references.
type RecallResponse struct {
	SchemaVersion  string            `json:"schema_version"`
	Trust          ReferenceTrust    `json:"trust"`
	Scope          Scope             `json:"scope"`
	ScopeChangeSeq uint64            `json:"scope_change_seq"`
	Results        []RecallReference `json:"results"`
}
