package memory

import "context"

// DenyCanonicalSourceBatch performs one bounded logical-forget step. It marks
// the source denied before any provenance scan, so recall remains fail-closed
// if a later batch or revision write is interrupted. Higher-level workers own
// repeated invocation, checkpoint persistence, physical scrubbing, and retry.
func DenyCanonicalSourceBatch(
	ctx context.Context,
	store CanonicalSourceForgetStore,
	request CanonicalSourceForgetBatchRequest,
) (CanonicalSourceForgetBatchResult, error) {
	if ctx == nil {
		return CanonicalSourceForgetBatchResult{}, PermanentError(
			CodeInvalidDerived,
			"canonical source forget context is required",
			nil,
		)
	}

	if store == nil {
		return CanonicalSourceForgetBatchResult{}, PermanentError(
			CodeStoreFailure,
			"canonical source forget store is required",
			nil,
		)
	}

	if err := request.Validate(); err != nil {
		return CanonicalSourceForgetBatchResult{}, err
	}

	if err := store.DenySource(ctx, request.Scope, request.SourceID, request.DeniedAt); err != nil {
		return CanonicalSourceForgetBatchResult{}, err
	}

	revisionIDs, nextCursor, err := store.SourceRevisionBatch(
		ctx,
		request.Scope,
		request.SourceID,
		request.AfterRevisionID,
		request.Limit,
	)
	if err != nil {
		return CanonicalSourceForgetBatchResult{}, err
	}

	if err := denyCanonicalRevisionBatch(ctx, store, request, revisionIDs); err != nil {
		return CanonicalSourceForgetBatchResult{}, err
	}

	if nextCursor != "" && !isCanonicalID(nextCursor) {
		return CanonicalSourceForgetBatchResult{}, PermanentError(
			CodeStoreFailure,
			"canonical source forget store returned an invalid cursor",
			nil,
		)
	}

	return CanonicalSourceForgetBatchResult{
		RevisionIDs: append(
			[]string(
				nil,
			),
			revisionIDs...,
		),
		NextCursor: nextCursor,
	}, nil
}

func denyCanonicalRevisionBatch(
	ctx context.Context,
	store CanonicalSourceForgetStore,
	request CanonicalSourceForgetBatchRequest,
	revisionIDs []string,
) error {
	for _, revisionID := range revisionIDs {
		if !isCanonicalID(revisionID) {
			return PermanentError(
				CodeStoreFailure,
				"canonical source forget store returned an invalid revision identity",
				nil,
			)
		}

		if err := store.DenyRevision(ctx, request.Scope, revisionID, request.DeniedAt); err != nil {
			return err
		}
	}

	return nil
}
