package badger

import (
	"bufio"
	"context"
	"hash"

	"github.com/dgraph-io/badger/v4"
)

func (s *CanonicalStore) exportCanonicalLogicalRecords(ctx context.Context, writer *bufio.Writer, digest hash.Hash) (uint64, error) {
	var count uint64
	err := s.db.View(func(txn *badger.Txn) error {
		iterator := txn.NewIterator(badger.DefaultIteratorOptions)
		defer iterator.Close()
		for iterator.Rewind(); iterator.Valid(); iterator.Next() {
			if err := checkCanonicalMaintenanceContext(ctx); err != nil {
				return err
			}
			written, err := writeCanonicalLogicalExportRecord(writer, digest, iterator.Item())
			if err != nil {
				return err
			}
			if written {
				count++
			}
		}
		return nil
	})
	return count, err
}
