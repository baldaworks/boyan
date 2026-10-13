package badger

import (
	"context"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

type badgerDeniedRevision struct {
	RevisionID string    `json:"revision_id"`
	DeniedAt   time.Time `json:"denied_at"`
}

// DenyRevision commits a fail-closed cascade result for one revision.
func (s *CanonicalStore) DenyRevision(
	ctx context.Context,
	scope memory.Scope,
	revisionID string,
	deniedAt time.Time,
) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}

	if err := scope.Validate(); err != nil {
		return err
	}

	if revisionID == "" || deniedAt.IsZero() {
		return memory.PermanentError(memory.CodeInvalidDerived, "revision deny request is invalid", nil)
	}

	if s == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}

	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()

	if s.db == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}

	key, err := canonicalKey(scope, badgerRecordDeniedRevision, revisionID)
	if err != nil {
		return err
	}

	denied := badgerDeniedRevision{RevisionID: revisionID, DeniedAt: deniedAt.UTC()}

	if err := s.db.Update(func(txn *badger.Txn) error {
		if err := checkScopeEraseFence(txn, scope, ""); err != nil {
			return err
		}

		return putBadgerDenyRecord(txn, key, badgerRecordDeniedRevision, denied)
	}); err != nil {
		return canonicalStoreError("deny canonical revision", err)
	}

	return nil
}

// IsRevisionDenied lets recall fail closed while physical scrub is pending.
func (s *CanonicalStore) IsRevisionDenied(ctx context.Context, scope memory.Scope, revisionID string) (bool, error) {
	var denied badgerDeniedRevision

	return s.isDenied(
		ctx,
		scope,
		revisionID,
		badgerRecordDeniedRevision,
		"read canonical revision deny",
		&denied,
	)
}
