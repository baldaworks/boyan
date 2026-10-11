package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func (s *CanonicalStore) listCanonicalRevisionRefs(
	scope memory.Scope,
	prefix []byte,
	afterRevisionID string,
	limit uint32,
) ([]memory.RevisionRef, string, error) {
	refs := make([]memory.RevisionRef, 0, limit)
	next := ""
	err := s.db.View(func(txn *badger.Txn) error {
		iterator := txn.NewIterator(badger.DefaultIteratorOptions)
		defer iterator.Close()

		for iterator.Seek(prefix); iterator.ValidForPrefix(prefix) && uint32(len(refs)) < limit; iterator.Next() {
			revision, skip, err := scanCanonicalRevisionRef(txn, scope, iterator.Item().Key(), afterRevisionID)
			if err != nil {
				return err
			}

			if skip {
				continue
			}

			refs = append(refs, revision)
			next = revision.RevisionID
		}

		return nil
	})

	return refs, next, err
}
