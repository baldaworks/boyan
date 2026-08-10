package badger

import (
	"sync"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// CanonicalStore owns one canonical Badger directory. Store methods are split
// by responsibility so lifecycle, operation replay, and mutation persistence
// do not accrete in one file.
type CanonicalStore struct {
	db   *badger.DB
	gcMu sync.Mutex
	// maintenanceMu excludes lifecycle/maintenance operations while allowing
	// independent canonical mutations to use Badger's own concurrency control.
	maintenanceMu                 sync.RWMutex
	beforeCanonicalMutationCommit func() error // test fault-injection seam; nil in production.
}

// DB exposes the owned handle only to composition adapters that need a
// backend-specific maintenance operation. Callers should prefer the
// storage-neutral ports implemented by the store.
func (s *CanonicalStore) DB() *badger.DB {
	if s == nil {
		return nil
	}
	return s.db
}

var _ memory.CanonicalSourceForgetStore = (*CanonicalStore)(nil)
var _ memory.CanonicalStore = (*CanonicalStore)(nil)
var _ memory.ProjectionCheckpointStore = (*CanonicalStore)(nil)
var _ memory.ProjectionRetentionFloorWriter = (*CanonicalStore)(nil)
var _ memory.ScopeCheckpointStore = (*CanonicalStore)(nil)
var _ memory.CanonicalOperationReader = (*CanonicalStore)(nil)
var _ memory.CanonicalOperationCommitter = (*CanonicalStore)(nil)
