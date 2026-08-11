package badger

import (
	"context"
	"errors"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

type badgerProjectionRetention struct {
	Floor uint64 `json:"floor"`
}

// LoadProjectionRetentionFloor returns the minimum watermark that remains safe
// for incremental replay after retention removed older change records. A
// missing floor means that all canonical changes are still available and is
// represented as zero.
func (s *CanonicalStore) LoadProjectionRetentionFloor(ctx context.Context, scope memory.Scope, projectionID string) (uint64, error) {
	if err := canonicalContextError(ctx); err != nil {
		return 0, err
	}
	if err := scope.Validate(); err != nil || strings.TrimSpace(projectionID) == "" {
		return 0, memory.PermanentError(memory.CodeInvalidScope, "projection retention lookup is invalid", err)
	}
	key, err := badgerProjectionRetentionKey(scope, projectionID)
	if err != nil {
		return 0, err
	}
	var retention badgerProjectionRetention
	err = s.db.View(func(txn *badger.Txn) error {
		return getCanonicalRecord(txn, key, badgerRecordProjectionRetention, &retention)
	})
	if errors.Is(err, badger.ErrKeyNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, canonicalStoreError("load projection retention floor", err)
	}
	return retention.Floor, nil
}
