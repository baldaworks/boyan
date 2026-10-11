package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Start launches the single maintenance worker. A second start is rejected so
// one canonical owner cannot accidentally schedule duplicate GC loops.
func (m *CanonicalMaintenance) Start(ctx context.Context) error {
	if m == nil || m.store == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical maintenance is unavailable", nil)
	}

	if ctx == nil {
		return memory.PermanentError(memory.CodeInvalidDerived, "canonical maintenance context is required", nil)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return memory.PermanentError(memory.CodeConflict, "canonical maintenance is already running", nil)
	}

	workerCtx, cancel := context.WithCancel(ctx)
	m.running = true
	m.cancel = cancel
	m.done = make(chan struct{})
	m.lastErr = nil

	done := m.done
	go m.run(workerCtx, done)

	return nil
}
