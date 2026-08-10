package memory

import (
	"context"
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
	SessionID         string        `json:"session_id,omitempty"`
	MemoryKey         MemoryKey     `json:"memory_key,omitempty"`
	MinScopeChangeSeq uint64        `json:"min_scope_change_seq,omitempty"`
}

// RecallProjectionDocument is the rebuildable input to the lexical projection.
type RecallProjectionDocument struct {
	Scope          Scope
	ItemID         string
	RevisionID     string
	Revision       uint64
	Kind           MemoryKind
	Category       *AtomCategory
	MemoryKey      MemoryKey
	Text           string
	CreatedAt      time.Time
	Temporal       Temporal
	Sensitivity    Sensitivity
	Retention      RetentionClass
	SourceIDs      []string
	SessionIDs     []string
	ScopeChangeSeq uint64
}

// RecallProjectionHit is the bounded metadata returned by a projection.
type RecallProjectionHit struct {
	Scope          Scope
	ItemID         string
	RevisionID     string
	Revision       uint64
	Score          float64
	ScopeChangeSeq uint64
}

// RecallRecord is one canonical record hydrated for recall.
type RecallRecord struct {
	Scope             Scope
	ItemID            string
	RevisionID        string
	Revision          uint64
	Kind              MemoryKind
	Category          *AtomCategory
	LegacyKind        *DerivedKind
	LegacyItemID      string
	LegacyRevisionID  string
	LegacyOperationID string
	LegacySupersedes  *RevisionRef
	LegacyParents     []RevisionRef
	TopicKey          string
	Title             string
	MemoryKey         MemoryKey
	Text              string
	State             RevisionState
	CreatedAt         time.Time
	Temporal          Temporal
	Sensitivity       Sensitivity
	Retention         RetentionClass
	Evidence          []EvidenceRef
	SourceIDs         []string
	SourceRefs        []SourceRef
	SessionIDs        []string
	ScopeChangeSeq    uint64
}

// RecallCanonicalReader is the bounded canonical hydration/fallback port.
type RecallCanonicalReader interface {
	LoadRecallRecords(ctx context.Context, scope Scope, revisionIDs []string) ([]RecallRecord, error)
	SearchRecallTail(ctx context.Context, request RecallRequest, limit uint32) ([]RecallRecord, error)
	CurrentScopeChangeSeq(ctx context.Context, scope Scope) (uint64, error)
}

// RecallProjection is implemented by the rebuildable lexical projection.
type RecallProjection interface {
	SearchRecall(ctx context.Context, request RecallRequest) ([]RecallProjectionHit, error)
}

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
