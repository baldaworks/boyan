package standaloneruntime

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

// Local projection ownership is required even when canonical state is remote
// or two callers choose distinct canonical files within the same state root.
func lockStandaloneState(layout stateLayout) (*flock.Flock, error) {
	path := filepath.Join(layout.Root, ".boyan.lock")
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("Boyan state ownership path must be an ordinary file")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect Boyan state ownership: %w", err)
	}

	lock := flock.New(path)
	held, err := lock.TryLock()
	if err != nil || !held {
		_ = lock.Close()

		return nil, fmt.Errorf("Boyan local state is already owned or unavailable")
	}

	return lock, nil
}
