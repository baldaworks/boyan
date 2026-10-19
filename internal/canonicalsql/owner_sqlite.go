package canonicalsql

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/gofrs/flock"
	_ "modernc.org/sqlite" // SQLite canonical storage uses the existing pure-Go driver.
)

const (
	defaultBusyTimeout   = time.Second
	maxBusyTimeout       = 5 * time.Second
	privateDirectoryMode = 0o700
)

// SQLiteConfig selects an on-disk canonical file, separate from search state.
// BusyTimeout is bounded to five seconds; zero selects one second.
type SQLiteConfig struct {
	Path        string
	BusyTimeout time.Duration
}

// Validate checks ordinary-path and timeout bounds without filesystem changes.
func (config SQLiteConfig) Validate() error { return config.validate() }

func (config SQLiteConfig) validate() error {
	if strings.TrimSpace(config.Path) == "" || config.Path == ":memory:" || strings.HasPrefix(config.Path, "file:") {
		return invalid("invalid canonical SQLite path")
	}

	if config.BusyTimeout < 0 || config.BusyTimeout > maxBusyTimeout ||
		(config.BusyTimeout > 0 && config.BusyTimeout < time.Millisecond) {
		return invalid("invalid canonical SQLite busy timeout")
	}

	return nil
}

// OpenSQLiteOwner acquires exclusive file ownership before configuring the
// pinned connection or migrating. It never imports or removes existing state.
func OpenSQLiteOwner(ctx context.Context, config SQLiteConfig) (*Owner, error) {
	if ctx == nil {
		return nil, invalid("canonical SQLite context is required")
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := config.validate(); err != nil {
		return nil, err
	}

	if config.BusyTimeout == 0 {
		config.BusyTimeout = defaultBusyTimeout
	}

	path, lock, err := lockSQLiteFile(config.Path)
	if err != nil {
		return nil, err
	}

	owner, err := openSQLiteConnection(ctx, path, config.BusyTimeout, lock)
	if err != nil {
		_ = lock.Close()
	}

	return owner, err
}

func lockSQLiteFile(path string) (string, *flock.Flock, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", nil, invalid("invalid canonical SQLite path")
	}

	if err := os.MkdirAll(filepath.Dir(absolute), privateDirectoryMode); err != nil {
		return "", nil, databaseError(context.Background(), "create SQLite directory", err)
	}

	// Lock the database inode itself, never an alias-dependent sidecar. flock
	// creates a missing file with mode 0600 and never truncates/unlinks it.
	lock := flock.New(absolute)
	locked, err := lock.TryLock()
	if err != nil || !locked {
		_ = lock.Close()

		return "", nil, ownerUnavailable()
	}

	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		_ = lock.Close()

		return "", nil, invalid("invalid canonical SQLite file")
	}

	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		_ = lock.Close()

		return "", nil, invalid("canonical SQLite path must be a regular file")
	}

	return resolved, lock, nil
}

func openSQLiteConnection(ctx context.Context, path string, timeout time.Duration, lock *flock.Flock) (*Owner, error) {
	uri := url.URL{Scheme: "file", Path: path}
	query := url.Values{"mode": {"rw"}}
	uri.RawQuery = query.Encode()

	pool, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, databaseError(ctx, "open SQLite", err)
	}

	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)

	conn, err := pool.Conn(ctx)
	if err != nil {
		_ = pool.Close()

		return nil, databaseError(ctx, "connect SQLite", err)
	}

	connection := NewSQLite(conn)
	if err := configureSQLite(ctx, connection, timeout); err != nil {
		_ = conn.Close()
		_ = pool.Close()

		return nil, err
	}

	if err := connection.Migrate(ctx); err != nil {
		_ = conn.Close()
		_ = pool.Close()

		return nil, err
	}

	return newOwner(connection, func() error {
		cleanup, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
		defer cancel()

		checkpointErr := checkpointSQLite(cleanup, connection)
		closeErr := errors.Join(conn.Close(), pool.Close())
		lockErr := lock.Close()

		return errors.Join(
			checkpointErr,
			databaseError(
				cleanup,
				"close SQLite",
				closeErr,
			),
			databaseError(cleanup, "release SQLite ownership", lockErr),
		)
	}), nil
}

func checkpointSQLite(ctx context.Context, connection *Connection) error {
	var busy, remaining, checkpointed int

	err := scanOne(ctx, connection.q, "PRAGMA wal_checkpoint(TRUNCATE)", nil, &busy, &remaining, &checkpointed)
	if err != nil {
		return databaseError(ctx, "SQLite checkpoint", err)
	}

	if busy != 0 {
		return memory.RetryableError(memory.CodeConflict, "canonical SQLite checkpoint busy", nil)
	}

	return nil
}

func configureSQLite(ctx context.Context, connection *Connection, timeout time.Duration) error {
	for _, statement := range []string{
		"PRAGMA busy_timeout=" + strconv.FormatInt(timeout.Milliseconds(), 10),
		"PRAGMA journal_mode=WAL", "PRAGMA synchronous=FULL", "PRAGMA foreign_keys=ON",
		"PRAGMA ignore_check_constraints=OFF",
	} {
		if _, err := connection.q.exec(ctx, statement); err != nil {
			return databaseError(ctx, "configure SQLite", err)
		}
	}

	return nil
}
