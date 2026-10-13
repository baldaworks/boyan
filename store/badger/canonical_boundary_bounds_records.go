package badger

import (
	"context"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func (r *CanonicalReader) resolveBoundaryRecordBounds(ctx context.Context, scope memory.Scope,
	records []memory.RecallRecord, at time.Time) ([]memory.RecallRecord, error) {
	r.store.maintenanceMu.RLock()
	defer r.store.maintenanceMu.RUnlock()

	if r.store.db == nil {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "canonical reader is closed", nil)
	}

	err := r.store.db.View(func(txn *badger.Txn) error {
		resolver := newBoundaryBoundsResolver(ctx, txn, scope, at)

		for index, record := range records {
			ref := memory.RevisionRef{ItemID: record.ItemID, RevisionID: record.RevisionID}
			if err := resolver.requireCurrent(ref); err != nil {
				return err
			}

			parent, err := resolver.resolve(ref)
			if err != nil {
				return err
			}

			records[index].Temporal = cloneBoundaryTemporal(parent.Temporal)
			records[index].Sensitivity = parent.Sensitivity
			records[index].Retention = parent.Retention
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return records, nil
}
