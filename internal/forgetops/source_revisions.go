package forgetops

import (
	"context"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (a *CanonicalForgetAdapter) sourceRevisionIDs(
	ctx context.Context,
	scope memory.Scope,
	sourceID string,
	forgottenAt time.Time,
) (
	[]string,
	error,
) {
	var revisionIDs []string

	cursor := ""
	for {
		result, err := memory.DenyCanonicalSourceBatch(ctx, a.store, memory.CanonicalSourceForgetBatchRequest{
			Scope:           scope,
			SourceID:        sourceID,
			AfterRevisionID: cursor,
			Limit:           forgetBatchLimit,
			DeniedAt:        forgottenAt,
		})
		if err != nil {
			return nil, err
		}

		revisionIDs = appendUniqueStrings(revisionIDs, result.RevisionIDs)
		if result.NextCursor == "" {
			return revisionIDs, nil
		}

		cursor = result.NextCursor
	}
}
