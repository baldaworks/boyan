package forgetops

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

const pendingErasePageSize = 128

// RecoverPendingScopeErases drains original durable operations before readiness.
// A compatibility rejection is safe only after its locks and fence are released.
func RecoverPendingScopeErases(ctx context.Context, store CanonicalEraseRecoveryStore) error {
	if err := prepareEraseRecovery(ctx, store); err != nil {
		return err
	}

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

func resumeEraseJournal(ctx context.Context, store CanonicalEraseRecoveryStore,
	journal memory.ScopeEraseJournal) error {
	for journal.Stage != memory.ScopeEraseComplete && journal.Stage != memory.ScopeEraseRejected {
		next, err := store.AdvanceScopeErase(ctx, journal.Scope, journal.OperationID)
		if err != nil && next.Stage != memory.ScopeEraseRejected {
			return err
		}

		journal = next
	}

	return nil
}

// CanonicalEraseRecoveryStore exposes only the durable work recovery consumes.
type CanonicalEraseRecoveryStore interface {
	ListPendingScopeErases(ctx context.Context, cursor []byte, limit int) ([]memory.ScopeEraseJournal, []byte, error)
	AdvanceScopeErase(ctx context.Context, scope memory.Scope, operationID string) (memory.ScopeEraseJournal, error)
}

func prepareEraseRecovery(ctx context.Context, store CanonicalEraseRecoveryStore) error {
	if preflight, ok := store.(interface {
		CheckIntegrity(ctx context.Context) error
	}); ok {
		return preflight.CheckIntegrity(ctx)
	}

	return memory.PermanentError(memory.CodeStoreFailure, "canonical erase recovery preflight is unavailable", nil)
}
