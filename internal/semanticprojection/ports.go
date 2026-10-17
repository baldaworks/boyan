package semanticprojection

import (
	"context"

	semantic "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

// SearchReader opens the closable shared physical view used by SQL recall.
type SearchReader interface {
	OpenSearchSnapshot(ctx context.Context, scope memory.Scope) (semantic.SearchSnapshot, error)
	LoadActivePhysicalProjectionManifest(ctx context.Context, scope memory.Scope,
		projectionID string) (memory.ProjectionManifest, bool, error)
}

// BuildStore is the manager's storage port. Reuse is bounded and build-only;
// readiness inspects observation metadata without returning vectors.
type BuildStore interface {
	ActiveState(ctx context.Context, scope memory.Scope) (semantic.State, bool, error)
	Stage(ctx context.Context, generation semantic.Generation, coverage []semantic.CoverageRecord,
		chunks []semantic.ProjectionChunk) (semantic.State, error)
	Publish(ctx context.Context, publication semantic.Publication) error
	PublishDegraded(ctx context.Context, marker semantic.Degraded, expectedBuildID string) (semantic.State, error)
	LoadReusable(ctx context.Context, view semantic.View) (semantic.Reusable, bool, error)
	ObservedMetadataReady(ctx context.Context, view semantic.View) (bool, error)
	CollectGarbage(ctx context.Context) error
}

// CandidateLifecycle manages owned candidates independently of source repair.
// Startup orphan repair requires a quiescent producer admission barrier.
type CandidateLifecycle interface {
	ReleaseUnpublished(ctx context.Context, scope memory.Scope, buildID string) (semantic.CandidateRelease, error)
	InvalidatePublication(ctx context.Context, scope memory.Scope, buildID string, epoch uint64) error
	RepairSemanticOrphans(ctx context.Context) error
}
