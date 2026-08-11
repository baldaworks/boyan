package badger

import (
	"context"
	"strings"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

type badgerDeniedSource struct {
	SourceID string    `json:"source_id"`
	DeniedAt time.Time `json:"denied_at"`
}

// DenySource commits the fail-closed logical forget marker before any
// asynchronous provenance traversal or payload scrubbing begins.
func (s *CanonicalStore) DenySource(ctx context.Context, scope memory.Scope, sourceID string, deniedAt time.Time) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}
	if err := scope.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(sourceID) != sourceID || sourceID == "" || deniedAt.IsZero() {
		return memory.PermanentError(memory.CodeInvalidDerived, "source deny request is invalid", nil)
	}
	if s == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	key, err := canonicalKey(scope, badgerRecordDeniedSource, sourceID)
	if err != nil {
		return err
	}
	denied := badgerDeniedSource{SourceID: sourceID, DeniedAt: deniedAt.UTC()}
	if err := s.db.Update(func(txn *badger.Txn) error {
		return putBadgerDenyRecord(txn, key, badgerRecordDeniedSource, denied)
	}); err != nil {
		return canonicalStoreError("deny canonical source", err)
	}
	return nil
}

// IsSourceDenied lets recall and traversal fail closed before physical scrub.
func (s *CanonicalStore) IsSourceDenied(ctx context.Context, scope memory.Scope, sourceID string) (bool, error) {
	var denied badgerDeniedSource
	return s.isDenied(ctx, scope, sourceID, badgerRecordDeniedSource, "read canonical source deny", &denied)
}
