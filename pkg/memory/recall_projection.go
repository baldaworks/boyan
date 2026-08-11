package memory

import (
	"context"
	"time"
)

// RecallProjectionDocument is the rebuildable input to the lexical projection.
type RecallProjectionDocument struct {
	Scope           Scope
	ItemID          string
	RevisionID      string
	Revision        uint64
	Kind            MemoryKind
	Category        *AtomCategory
	MemoryKey       MemoryKey
	Text            string
	CreatedAt       time.Time
	Temporal        Temporal
	Sensitivity     Sensitivity
	Retention       RetentionClass
	SourceIDs       []string
	ConversationIDs []string
	ScopeChangeSeq  uint64
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
