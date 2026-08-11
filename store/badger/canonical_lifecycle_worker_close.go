package badger

import "context"

// Close stops maintenance and then closes the sole Badger owner. The caller
// can use this as the composition-root OnStop hook.
func (m *CanonicalMaintenance) Close(ctx context.Context) error {
	if m == nil || m.store == nil {
		return nil
	}
	if err := m.Stop(ctx); err != nil {
		return err
	}
	return m.store.Close()
}
