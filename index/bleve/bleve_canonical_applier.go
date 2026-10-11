package bleve

import (
	"context"
	"sync"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// BleveCanonicalApplier adapts bounded canonical revisions to disposable
// Bleve generations. It never treats index content as authoritative: every
// revision is hydrated through the canonical reader before indexing.
type BleveCanonicalApplier struct {
	projection *BleveRecallProjection
	reader     memory.RecallCanonicalReader

	retention    projectionManifestWalker
	projectionID string
	mu           sync.Mutex
	generations  map[string]*BleveGeneration
}

func NewBleveCanonicalApplier(
	projection *BleveRecallProjection,
	reader memory.RecallCanonicalReader,
) (
	*BleveCanonicalApplier,
	error,
) {
	if projection == nil || reader == nil {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "bleve canonical applier dependencies are required", nil)
	}

	return &BleveCanonicalApplier{
		projection:  projection,
		reader:      reader,
		generations: make(map[string]*BleveGeneration),
	}, nil
}

// ScrubCanonicalForget removes matching disposable projection material after
// the canonical deny/CAS decision has committed.  Recall remains correct if
// this best-effort maintenance hook is interrupted because every candidate is
// revalidated against canonical denial state.
func (a *BleveCanonicalApplier) ScrubCanonicalForget(
	ctx context.Context,
	scope memory.Scope,
	sourceIDs, revisionIDs []string,
) error {
	if a == nil || a.projection == nil {
		return memory.PermanentError(memory.CodeDisabled, "bleve canonical applier is unavailable", nil)
	}

	return a.projection.ScrubCanonicalForget(ctx, scope, sourceIDs, revisionIDs)
}

var _ memory.ProjectionApplier = (*BleveCanonicalApplier)(nil)
var _ memory.ProjectionBatchRebuildApplier = (*BleveCanonicalApplier)(nil)
var _ memory.ProjectionGenerationActivator = (*BleveCanonicalApplier)(nil)
