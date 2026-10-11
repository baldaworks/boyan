package forgetops

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (a *CanonicalForgetAdapter) scrubForgetOutcome(
	ctx context.Context,
	scope memory.Scope,
	sourceIDs []string,
	revisions []memory.RevisionRef,
) error {
	revisionIDs := make([]string, 0, len(revisions))
	for _, revision := range revisions {
		revisionIDs = append(revisionIDs, revision.RevisionID)
	}

	for start := 0; start < len(sourceIDs) || start < len(revisionIDs); start += scrubBatchLimit {
		sourceBatch := chunkStrings(sourceIDs, start, scrubBatchLimit)

		revisionBatch := chunkStrings(revisionIDs, start, scrubBatchLimit)
		if err := a.store.ScrubCanonicalForget(ctx, scope, sourceBatch, revisionBatch); err != nil {
			return err
		}
	}

	return nil
}
