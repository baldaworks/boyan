package badger

import (
	"fmt"
	"strings"

	"github.com/dgraph-io/badger/v4"
)

// OpenCanonicalStore opens the sole writable canonical directory.
func OpenCanonicalStore(directory string) (*CanonicalStore, error) {
	return openWithConfig(directory, DefaultConfig())
}

func openWithConfig(directory string, config Config) (*CanonicalStore, error) {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return nil, fmt.Errorf("canonical badger directory is required")
	}

	if !config.SyncWrites {
		config.SyncWrites = true
	}

	if !config.DetectConflicts {
		config.DetectConflicts = true
	}

	if config.NumVersionsToKeep <= 0 {
		config.NumVersionsToKeep = 1
	}

	options := badger.DefaultOptions(directory).
		WithSyncWrites(config.SyncWrites).
		WithDetectConflicts(config.DetectConflicts).
		WithNumVersionsToKeep(config.NumVersionsToKeep)

	db, err := badger.Open(options)
	if err != nil {
		return nil, fmt.Errorf("open canonical badger store: %w", err)
	}

	if err := initializeEmptySourcePrivacy(db); err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("initialize canonical source privacy: %w", err)
	}

	store := &CanonicalStore{db: db}
	if err := store.loadRestoreFence(); err != nil {
		_ = db.Close()

		return nil, err
	}

	return store, nil
}
