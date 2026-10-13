package forgetops

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/badger"
)

const pendingErasePageSize = 128

// RecoverPendingScopeErases drains original durable operations before readiness.
// A compatibility rejection is safe only after its locks and fence are released.
func RecoverPendingScopeErases(ctx context.Context, store *badger.CanonicalStore) error {
	var cursor []byte
	for {
		journals, next, err := store.ListPendingScopeErases(ctx, cursor, pendingErasePageSize)
		if err != nil {
			return err
		}

		for _, journal := range journals {
			if err := resumeEraseJournal(ctx, store, journal); err != nil {
				return err
			}
		}

		if len(next) == 0 {
			return nil
		}

		cursor = next
	}
}

func resumeEraseJournal(ctx context.Context, store *badger.CanonicalStore, journal memory.ScopeEraseJournal) error {
	for journal.Stage != memory.ScopeEraseComplete && journal.Stage != memory.ScopeEraseRejected {
		next, err := store.AdvanceScopeErase(ctx, journal.Scope, journal.OperationID)
		if err != nil && next.Stage != memory.ScopeEraseRejected {
			return err
		}

		journal = next
	}

	return nil
}
