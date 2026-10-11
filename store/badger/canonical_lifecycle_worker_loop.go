package badger

import (
	"context"
	"time"
)

func (m *CanonicalMaintenance) run(ctx context.Context, done chan struct{}) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	defer close(done)
	defer func() {
		m.mu.Lock()
		m.running = false
		m.cancel = nil
		m.done = nil
		m.mu.Unlock()
	}()

	for {
		select {
		case <-ticker.C:
			if err := m.store.RunValueLogGC(m.ratio); err != nil {
				m.mu.Lock()
				m.lastErr = err
				m.mu.Unlock()
			}
		case <-ctx.Done():
			return
		}
	}
}

// LastError reports the latest non-fatal GC diagnostic. Value-log GC failures
// do not make canonical records writable or trigger destructive repair; the
// composition root can surface this diagnostic and choose to stop the owner.
func (m *CanonicalMaintenance) LastError() error {
	if m == nil {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	return m.lastErr
}
