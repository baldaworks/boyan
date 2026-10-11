package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Stop cancels maintenance and waits for the worker to release its ticker.
// The store remains open for other lifecycle work until Close is called.
func (m *CanonicalMaintenance) Stop(ctx context.Context) error {
	if m == nil {
		return nil
	}

	if ctx == nil {
		return memory.PermanentError(memory.CodeInvalidDerived, "canonical maintenance context is required", nil)
	}

	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()

		return nil
	}

	cancel := m.cancel
	done := m.done
	m.mu.Unlock()
	cancel()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return memory.RetryableError(
			memory.CodeTimeout,
			"canonical maintenance did not stop before context ended",
			ctx.Err(),
		)
	}
}
