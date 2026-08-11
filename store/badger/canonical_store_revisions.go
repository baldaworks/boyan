package badger

import (
	"context"
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func (s *CanonicalStore) LoadCanonicalRevisions(ctx context.Context, request memory.CanonicalRevisionReadRequest) ([]memory.MemoryRevision, error) {
	if err := canonicalContextError(ctx); err != nil {
		return nil, err
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	revisions := make([]memory.MemoryRevision, 0, len(request.RevisionIDs))
	err := s.db.View(func(txn *badger.Txn) error {
		for _, revisionID := range request.RevisionIDs {
			key, err := canonicalKey(request.Scope, badgerRecordRevision, revisionID)
			if err != nil {
				return err
			}
			var revision memory.MemoryRevision
			if err := getCanonicalRecord(txn, key, badgerRecordRevision, &revision); err != nil {
				if errors.Is(err, badger.ErrKeyNotFound) {
					return memory.PermanentError(memory.CodeNotFound, "canonical revision is missing", nil)
				}
				return err
			}
			if err := revision.Validate(); err != nil {
				return err
			}
			revisions = append(revisions, revision)
		}
		return nil
	})
	if err != nil {
		return nil, canonicalStoreError("load canonical revisions", err)
	}
	return revisions, nil
}
