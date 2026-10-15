package semantic

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dgraph-io/badger/v4"
	"golang.org/x/sync/semaphore"
)

const (
	directoryMode = 0o700
	fileMode      = 0o600
)

// Store owns one independent disposable Badger database.
type Store struct {
	gate                    *storeGate
	db                      *badger.DB
	directory               string
	closed                  bool
	afterStageBatch         func(int) error // test-only crash/cancellation seam
	afterGarbageScanBatch   func(int)       // test-only bounded-work seam
	afterGarbageDeleteBatch func(int)       // test-only bounded-work seam
}

const storeGateCapacity int64 = 1 << 30

type storeGate struct {
	weighted *semaphore.Weighted
}

func newStoreGate() *storeGate {
	return &storeGate{weighted: semaphore.NewWeighted(storeGateCapacity)}
}

func (gate *storeGate) acquire(ctx context.Context, write bool) (func(), error) {
	weight := int64(1)
	if write {
		weight = storeGateCapacity
	}

	if err := gate.weighted.Acquire(ctx, weight); err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		gate.weighted.Release(weight)

		return nil, err
	}

	return func() { gate.weighted.Release(weight) }, nil
}

// Open opens a semantic database at the caller-provided embeddings directory.
func Open(directory string) (*Store, error) {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return nil, fmt.Errorf("%w: directory is required", ErrInvalid)
	}

	directory, err := prepareDatabaseDirectory(directory)
	if err != nil {
		return nil, err
	}

	db, err := openBadgerDatabase(directory)
	if err != nil {
		return nil, err
	}

	store := &Store{gate: newStoreGate(), db: db, directory: directory}
	if err := store.validateActiveGenerations(context.Background()); err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("validate semantic projection startup state: %w", err)
	}

	if err := store.collectGarbage(context.Background()); err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("clean semantic projection startup state: %w", err)
	}

	if err := secureDatabaseTree(directory); err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("secure semantic projection files: %w", err)
	}

	return store, nil
}

func prepareDatabaseDirectory(directory string) (string, error) {
	absolute, err := nonSymlinkPath(directory)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(absolute, directoryMode); err != nil {
		return "", fmt.Errorf("create semantic projection directory: %w", err)
	}

	if _, err := nonSymlinkPath(absolute); err != nil {
		return "", err
	}

	if err := os.Chmod(absolute, directoryMode); err != nil {
		return "", fmt.Errorf("secure semantic projection directory: %w", err)
	}

	if _, err := nonSymlinkPath(absolute); err != nil {
		return "", err
	}

	// Badger accepts only path names, not an already-open directory handle.
	// Reject every symlink and special file already present before handing the
	// tree to Badger. Once the directory identity is stable, its private 0700
	// mode excludes other OS principals from replacing children. A caller must
	// still prevent concurrent replacement through writable ancestors or the
	// same principal. openBadgerDatabase revalidates immediately after Badger
	// opens the tree, but that is detection-in-depth, not a claim of race-free
	// path traversal.
	if err := validateDatabaseTree(absolute); err != nil {
		return "", fmt.Errorf("validate semantic projection files: %w", err)
	}

	if _, err := nonSymlinkPath(absolute); err != nil {
		return "", err
	}

	return absolute, nil
}

func openBadgerDatabase(directory string) (*badger.DB, error) {
	options := badger.DefaultOptions(directory).
		WithSyncWrites(true).
		WithDetectConflicts(true).
		WithNumVersionsToKeep(1).
		WithLogger(nil)

	database, err := badger.Open(options)
	if err != nil {
		return nil, fmt.Errorf("open semantic projection: %w", err)
	}

	if err := validateDatabaseTree(directory); err != nil {
		_ = database.Close()

		return nil, fmt.Errorf("revalidate opened semantic projection files: %w", err)
	}

	return database, nil
}

func nonSymlinkPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("%w: resolve semantic projection directory", ErrInvalid)
	}

	for current := filepath.Clean(absolute); ; current = filepath.Dir(current) {
		info, statErr := os.Lstat(current)
		switch {
		case statErr == nil && info.Mode()&os.ModeSymlink != 0:
			return "", fmt.Errorf("%w: semantic projection path contains a symlink", ErrInvalid)
		case statErr != nil && !errors.Is(statErr, os.ErrNotExist):
			return "", fmt.Errorf("inspect semantic projection path: %w", statErr)
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}

	return absolute, nil
}

// Close closes the owned Badger handle. It is safe to call more than once.
func (store *Store) Close() error {
	if store == nil {
		return nil
	}

	unlock, err := store.gate.acquire(context.Background(), true)
	if err != nil {
		return err
	}
	defer unlock()

	if store.closed {
		return nil
	}

	store.closed = true
	closeErr := store.db.Close()

	secureErr := secureDatabaseTree(store.directory)
	if err := errors.Join(closeErr, secureErr); err != nil {
		return fmt.Errorf("close semantic projection: %w", err)
	}

	return nil
}

func secureDatabaseTree(directory string) error {
	return filepath.WalkDir(directory, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symlink in semantic projection directory", ErrInvalid)
		}

		if entry.IsDir() {
			return os.Chmod(path, directoryMode)
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		if info.Mode().IsRegular() {
			return os.Chmod(path, fileMode)
		}

		return fmt.Errorf("%w: unsafe special file in semantic projection directory", ErrInvalid)
	})
}

func validateDatabaseTree(directory string) error {
	return filepath.WalkDir(directory, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symlink in semantic projection directory", ErrInvalid)
		}

		if entry.IsDir() {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		if !info.Mode().IsRegular() {
			return fmt.Errorf("%w: unsafe special file in semantic projection directory", ErrInvalid)
		}

		return nil
	})
}

func (store *Store) ensureOpen() error {
	if store == nil || store.closed || store.db == nil {
		return ErrClosed
	}

	return nil
}
