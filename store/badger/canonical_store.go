package badger

import (
	"sync"
	"sync/atomic"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// CanonicalStore owns one canonical Badger directory. Store methods are split
// by responsibility so lifecycle, operation replay, and mutation persistence
// do not accrete in one file.
type CanonicalStore struct {
	db             *badger.DB
	restoreBlocked atomic.Bool
	restorePhase   string
	gcMu           sync.Mutex
	// maintenanceMu excludes lifecycle/maintenance operations while allowing
	// independent canonical mutations to use Badger's own concurrency control.
	maintenanceMu                 sync.RWMutex
	beforeRestoreDBSync           func() error                         // Test-only persistence failure seam.
	beforeRestoreFenceWrite       func(string) error                   // Test-only fence persistence failure seam.
	afterNativeRestoreReplay      func() error                         // Test-only restrictive replay cancellation seam.
	afterNativeRestoreLoad        func() error                         // Test-only post-Load cancellation/crash boundary.
	afterSourcePrivacyBatch       func(string, int) error              // Test-only bounded batch/cancellation seam.
	beforeScopeEraseBatchCommit   func(memory.ScopeEraseJournal) error // test fault-injection seam; nil in production.
	beforeCanonicalMutationCommit func() error                         // test fault-injection seam; nil in production.
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
