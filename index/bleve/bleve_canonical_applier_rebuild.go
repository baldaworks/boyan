package bleve

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (a *BleveCanonicalApplier) BeginProjectionRebuild(ctx context.Context, _ memory.Scope, generationID string) error {
	_, err := a.generation(ctx, generationID)

	return err
}

func (a *BleveCanonicalApplier) ApplyProjectionRebuildBatch(
	ctx context.Context,
	scope memory.Scope,
	generationID string,
	view memory.BoundedProjectionView,
) error {
	if err := view.Validate(); err != nil {
		return err
	}

	revisions := make([]memory.MemoryRevision, 0, len(view.Active))
	for _, active := range view.Active {
		revisions = append(revisions, active.Revision)
	}

	return a.Apply(ctx, scope, generationID, nil, revisions)
}

func (a *BleveCanonicalApplier) EndProjectionRebuild(context.Context, memory.Scope, string) error {
	return nil
}
