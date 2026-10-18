package sqlite

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"time"
)

var (
	_ memory.CanonicalStore                 = (*Store)(nil)
	_ memory.ProjectionCheckpointStore      = (*Store)(nil)
	_ memory.ProjectionRetentionFloorWriter = (*Store)(nil)
	_ memory.ScopeCheckpointStore           = (*Store)(nil)
)

// ClaimDeliveryOutbox delegates atomic ordered lease acquisition.
func (s *Store) ClaimDeliveryOutbox(
	ctx context.Context,
	request memory.DeliveryClaimRequest,
) ([]memory.ClaimedDelivery, error) {
	return s.canonicalOwner().ClaimDeliveryOutbox(ctx, request)
}

// SettleDeliveryOutbox delegates exact-owner lease settlement.
func (s *Store) SettleDeliveryOutbox(ctx context.Context, request memory.DeliverySettlementRequest) error {
	return s.canonicalOwner().SettleDeliveryOutbox(ctx, request)
}

// LoadProjectionManifest delegates exact generation lookup.
func (s *Store) LoadProjectionManifest(
	ctx context.Context,
	scope memory.Scope,
	projectionID, generationID string,
) (memory.ProjectionManifest, bool, error) {
	return s.canonicalOwner().LoadProjectionManifest(ctx, scope, projectionID, generationID)
}

// LoadActiveProjectionManifest delegates snapshot-consistent active lookup.
func (s *Store) LoadActiveProjectionManifest(
	ctx context.Context,
	scope memory.Scope,
	projectionID string,
) (memory.ProjectionManifest, bool, error) {
	return s.canonicalOwner().LoadActiveProjectionManifest(ctx, scope, projectionID)
}

// MarkProjectionDirty delegates generation repair metadata.
func (s *Store) MarkProjectionDirty(ctx context.Context, manifest memory.ProjectionManifest) error {
	return s.canonicalOwner().MarkProjectionDirty(ctx, manifest)
}

// AdvanceProjectionWatermark delegates the monotone dirty-generation update.
func (s *Store) AdvanceProjectionWatermark(
	ctx context.Context,
	manifest memory.ProjectionManifest,
	watermark uint64,
	updatedAt time.Time,
) error {
	return s.canonicalOwner().AdvanceProjectionWatermark(ctx, manifest, watermark, updatedAt)
}

// ActivateProjectionGeneration delegates atomic publication and retirement.
func (s *Store) ActivateProjectionGeneration(
	ctx context.Context,
	manifest memory.ProjectionManifest,
	updatedAt time.Time,
) error {
	return s.canonicalOwner().ActivateProjectionGeneration(ctx, manifest, updatedAt)
}

// LoadProjectionRetentionFloor delegates the durable incremental replay floor.
func (s *Store) LoadProjectionRetentionFloor(
	ctx context.Context,
	scope memory.Scope,
	projectionID string,
) (uint64, error) {
	return s.canonicalOwner().LoadProjectionRetentionFloor(ctx, scope, projectionID)
}

// SetProjectionRetentionFloor delegates monotone floor maintenance.
func (s *Store) SetProjectionRetentionFloor(
	ctx context.Context,
	scope memory.Scope,
	projectionID string,
	floor uint64,
) error {
	return s.canonicalOwner().SetProjectionRetentionFloor(ctx, scope, projectionID, floor)
}

// LoadScopeCheckpoint delegates the existing trigger checkpoint family.
func (s *Store) LoadScopeCheckpoint(
	ctx context.Context,
	scope memory.Scope,
	kind memory.ScopeCheckpointKind,
) (memory.ScopeCheckpoint, bool, error) {
	return s.canonicalOwner().LoadScopeCheckpoint(ctx, scope, kind)
}

// SaveScopeCheckpoint delegates atomic monotone trigger persistence.
func (s *Store) SaveScopeCheckpoint(ctx context.Context, checkpoint memory.ScopeCheckpoint) error {
	return s.canonicalOwner().SaveScopeCheckpoint(ctx, checkpoint)
}

// WalkProjectionManifests delegates snapshot manifest/reference enumeration.
// Callbacks must not mutate the canonical store.
func (s *Store) WalkProjectionManifests(
	ctx context.Context,
	projectionID string,
	visit func(memory.ProjectionManifest, bool) error,
) error {
	return s.canonicalOwner().WalkProjectionManifests(ctx, projectionID, visit)
}
