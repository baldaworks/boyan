package projectionsync //nolint:testpackage // Exercises shared private test fixtures within the package.

import (
	"context"
	"time"

	. "github.com/baldaworks/boyan/pkg/memory"
)

type projectionCanonicalStore struct {
	CanonicalStore

	changes []ScopeChange
}

func (s *projectionCanonicalStore) ScanScopeChanges(
	_ context.Context,
	_ Scope,
	after uint64,
	limit uint32,
) (
	[]ScopeChange,
	error,
) {
	changes := make([]ScopeChange, 0, limit)
	for _, change := range s.changes {
		if change.Sequence > after && uint32(len(changes)) < limit {
			changes = append(changes, change)
		}
	}

	return changes, nil
}

func (s *projectionCanonicalStore) LoadCanonicalRevisions(
	_ context.Context,
	request CanonicalRevisionReadRequest,
) (
	[]MemoryRevision,
	error,
) {
	revisions := make([]MemoryRevision, 0, len(request.RevisionIDs))
	for _, id := range request.RevisionIDs {
		revisions = append(revisions, MemoryRevision{RevisionID: id})
	}

	return revisions, nil
}

type projectionCheckpointStore struct {
	manifest      ProjectionManifest
	found         bool
	activateCount int
}

func (s *projectionCheckpointStore) LoadProjectionManifest(
	_ context.Context,
	_ Scope,
	_ string,
	_ string,
) (
	ProjectionManifest,
	bool,
	error,
) {
	return s.manifest, s.found, nil
}

func (s *projectionCheckpointStore) LoadActiveProjectionManifest(
	_ context.Context,
	_ Scope,
	_ string,
) (
	ProjectionManifest,
	bool,
	error,
) {
	return s.manifest, s.found && s.manifest.Status == ProjectionGenerationActive, nil
}

func (s *projectionCheckpointStore) MarkProjectionDirty(_ context.Context, manifest ProjectionManifest) error {
	manifest.Status = ProjectionGenerationDirty
	s.manifest = manifest
	s.found = true

	return nil
}

func (s *projectionCheckpointStore) AdvanceProjectionWatermark(
	_ context.Context,
	manifest ProjectionManifest,
	watermark uint64,
	updatedAt time.Time,
) error {
	s.manifest = manifest
	s.manifest.Status = ProjectionGenerationDirty
	s.manifest.Watermark = watermark
	s.manifest.UpdatedAt = updatedAt

	return nil
}

func (s *projectionCheckpointStore) ActivateProjectionGeneration(
	_ context.Context,
	manifest ProjectionManifest,
	updatedAt time.Time,
) error {
	s.manifest = manifest
	s.manifest.Status = ProjectionGenerationActive
	s.manifest.UpdatedAt = updatedAt
	s.activateCount++

	return nil
}

type projectionApplier struct {
	applyCount  int
	commitCount int
	applyErr    error
}

func (a *projectionApplier) Apply(_ context.Context, _ Scope, _ string, _ []ScopeChange, _ []MemoryRevision) error {
	a.applyCount++

	return a.applyErr
}

func (a *projectionApplier) Commit(_ context.Context, _ Scope, _ string) error {
	a.commitCount++

	return nil
}
