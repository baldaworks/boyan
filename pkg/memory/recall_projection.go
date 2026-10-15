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
	Lexical        float64
	Semantic       float64
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

// ConsistentRecallProjection certifies its pinned physical view through the
// request's minimum scope change sequence. Empty results are certified too.
// Adapters without this optional capability cannot serve a nonzero minimum.
type ConsistentRecallProjection interface {
	SearchRecallConsistent(ctx context.Context, request RecallRequest) ([]RecallProjectionHit, error)
}

// RecallProjectionCheckpointReader exposes authoritative active checkpoints.
type RecallProjectionCheckpointReader interface {
	LoadActiveProjectionManifest(ctx context.Context, scope Scope, projectionID string) (ProjectionManifest, bool, error)
}

// PhysicalProjectionViewReader certifies the generation that is actually open
// in a concrete projection adapter. A durable active checkpoint alone is not
// sufficient because physical generation activation can fail after checkpoint
// publication.
type PhysicalProjectionViewReader interface {
	LoadActivePhysicalProjectionManifest(
		ctx context.Context,
		scope Scope,
		projectionID string,
	) (ProjectionManifest, bool, error)
}

// CertifiedRecallResult binds bounded hits to the physical manifest sampled
// under the search adapter's lock. Uncertified results are lexical-only.
type CertifiedRecallResult struct {
	Hits      []RecallProjectionHit
	Manifest  ProjectionManifest
	Certified bool
}

// CertifiedRecallProjection searches an explicit bounded channel depth without
// applying the caller's final result limit.
type CertifiedRecallProjection interface {
	SearchRecallCertified(ctx context.Context, request RecallRequest, depth int) (CertifiedRecallResult, error)
}
