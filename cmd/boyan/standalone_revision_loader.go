package main

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/badger"
)

func loadCanonicalRevisionsByID(ctx context.Context, store *badger.CanonicalStore, scope memory.Scope, needed map[string]memory.RevisionRef) (map[string]memory.MemoryRevision, error) {
	if len(needed) == 0 {
		return map[string]memory.MemoryRevision{}, nil
	}
	ids := make([]string, 0, len(needed))
	for revisionID := range needed {
		ids = append(ids, revisionID)
	}
	loaded := make(map[string]memory.MemoryRevision, len(ids))
	for start := 0; start < len(ids); start += memory.MaxRecallCandidates {
		end := minInt(len(ids), start+memory.MaxRecallCandidates)
		revisions, err := store.LoadCanonicalRevisions(ctx, memory.CanonicalRevisionReadRequest{
			Scope:       scope,
			RevisionIDs: ids[start:end],
		})
		if err != nil {
			return nil, err
		}
		for _, revision := range revisions {
			loaded[revision.RevisionID] = revision
		}
	}
	return loaded, nil
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func chunkStrings(values []string, start, size int) []string {
	if start >= len(values) {
		return nil
	}
	end := minInt(len(values), start+size)
	return values[start:end]
}
