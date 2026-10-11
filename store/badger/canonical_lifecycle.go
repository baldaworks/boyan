package badger

import (
	"context"
	"sync"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

const (
	defaultCanonicalGCInterval = 6 * time.Hour
	defaultCanonicalDiscard    = 0.5
)

// CanonicalMaintenanceConfig controls the optional value-log maintenance
// worker. It is separate from the store so the composition root can start and
// stop maintenance in the same lifecycle as the canonical owner.
type CanonicalMaintenanceConfig struct {
	ValueLogGCInterval time.Duration
	DiscardRatio       float64
}

// CanonicalMaintenance owns one bounded maintenance goroutine for a Badger
// canonical store. It never repairs records and it never owns store closure.
type CanonicalMaintenance struct {
	store    *CanonicalStore
	interval time.Duration
	ratio    float64

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
	lastErr error
}

// NewCanonicalMaintenance validates operational defaults without opening a
// second database handle or changing the store's ownership boundary.
func NewCanonicalMaintenance(store *CanonicalStore, config CanonicalMaintenanceConfig) (*CanonicalMaintenance, error) {
	if store == nil {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "canonical maintenance store is required", nil)
	}

	interval := config.ValueLogGCInterval
	if interval == 0 {
		interval = defaultCanonicalGCInterval
	}

	if interval <= 0 {
		return nil, memory.PermanentError(memory.CodeInvalidDerived, "canonical maintenance interval must be positive", nil)
	}

	ratio := config.DiscardRatio
	if ratio == 0 {
		ratio = defaultCanonicalDiscard
	}

	if ratio <= 0 || ratio >= 1 {
		return nil, memory.PermanentError(
			memory.CodeInvalidDerived,
			"canonical maintenance discard ratio must be between zero and one",
			nil,
		)
	}

	return &CanonicalMaintenance{store: store, interval: interval, ratio: ratio}, nil
}
