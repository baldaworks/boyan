package forgetops

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/badger"
)

func listCanonicalSources(
	ctx context.Context,
	store *badger.CanonicalStore,
	scope memory.Scope,
) (
	[]memory.SourceRef,
	[]string,
	error,
) {
	sources := make([]memory.SourceRef, 0)
	sourceIDs := make([]string, 0)

	cursor := ""
	for {
		batch, next, err := store.ListCanonicalSourceRefs(ctx, scope, cursor, forgetBatchLimit)
		if err != nil {
			return nil, nil, err
		}

		for _, source := range batch {
			sources = append(sources, source)

			sourceID, err := store.CanonicalSourceID(ctx, scope, source)
			if err != nil {
				return nil, nil, err
			}

			sourceIDs = append(sourceIDs, sourceID)
		}

		if next == "" {
			return sources, sourceIDs, nil
		}

		cursor = next
	}
}

func listCanonicalRevisions(
	ctx context.Context,
	store *badger.CanonicalStore,
	scope memory.Scope,
) (
	[]memory.RevisionRef,
	[]string,
	error,
) {
	revisions := make([]memory.RevisionRef, 0)
	revisionIDs := make([]string, 0)

	cursor := ""
	for {
		batch, next, err := store.ListCanonicalRevisionRefs(ctx, scope, cursor, forgetBatchLimit)
		if err != nil {
			return nil, nil, err
		}

		revisions = append(revisions, batch...)
		for _, revision := range batch {
			revisionIDs = append(revisionIDs, revision.RevisionID)
		}

		if next == "" {
			return revisions, revisionIDs, nil
		}

		cursor = next
	}
}
