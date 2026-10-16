// Package sqlite owns Boyan's disposable, private SQLite search projection.
// Canonical state remains outside this package. No caller SQL or extension path
// is accepted, and the backend is not wired into runtime until its adapters exist.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"

	"github.com/baldaworks/boyan/pkg/memory"

	_ "modernc.org/sqlite"
	_ "modernc.org/sqlite/vec"
)

var (
	ErrClosed     = errors.New("sqlite search storage closed")
	ErrCorrupt    = errors.New("sqlite search storage corrupt")
	ErrSchema     = errors.New("sqlite search schema unsupported")
	ErrUnsafePath = errors.New("sqlite search path unsafe")
)

// Options selects schema initialization only. Pool sizes and durability are
// fixed implementation bounds; there is no configurable backend or disk quota.
type Options struct{ EmbeddingsEnabled bool }

const readerConnections = 2

// Store owns all SQL connections and operation cancellation. Close rejects new
// operations and drains canceled work before releasing the pools and root.
type Store struct {
	connectionLimits []sqlConnectionLimits
	stagingMu        sync.Mutex
	stagingOwners    map[memory.Scope]*SQLiteCanonicalApplier
	scopeWrites      sync.Map // memory.Scope -> chan struct{}; apply and scrub share ownership.
	writer, reader   *sql.DB
	root             *os.Root
	lifetime         context.Context
	cancel           context.CancelFunc
	mu               sync.Mutex
	checkpoints      memory.RecallProjectionCheckpointReader
	closing          bool
	work             sync.WaitGroup
	closeOnce        sync.Once
	closed           chan struct{}
	closeErr         error
}

// Open validates private paths and existing schema before allowing SQL writes.
// An unusable database is preserved for stopped operator backup/rebuild.
//
//nolint:cyclop,funlen // Every resource acquisition has an explicit failure cleanup.
func Open(ctx context.Context, stateDir string, options Options) (*Store, error) {
	if ctx == nil || stateDir == "" {
		return nil, fmt.Errorf("%w: invalid open input", ErrUnsafePath)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	root, path, fresh, err := privateSearchRoot(stateDir)
	if err != nil {
		return nil, openError(err)
	}

	if !fresh {
		if err := inspectExisting(ctx, path); err != nil {
			_ = root.Close()

			return nil, openError(err)
		}
	}

	writer, err := openPool(ctx, path, false, 1)
	if err != nil {
		_ = root.Close()

		return nil, openError(err)
	}

	if err := initializeSchema(ctx, writer, fresh, options.EmbeddingsEnabled); err != nil {
		_ = writer.Close()
		_ = root.Close()

		return nil, openError(err)
	}

	if fresh {
		// Persist the identity header before another opener validates the file.
		// This passive checkpoint runs outside publication and before readers.
		if _, err := writer.ExecContext(ctx, "PRAGMA wal_checkpoint(PASSIVE)"); err != nil {
			_ = writer.Close()
			_ = root.Close()

			return nil, openError(err)
		}
	}

	reader, err := openPool(ctx, path, true, readerConnections)
	if err != nil {
		_ = writer.Close()
		_ = root.Close()

		return nil, openError(err)
	}

	limits, err := inspectConnectionLimits(ctx, writer, 1)
	if err == nil {
		var readLimits []sqlConnectionLimits

		readLimits, err = inspectConnectionLimits(ctx, reader, readerConnections)
		limits = append(limits, readLimits...)
	}

	if err != nil {
		_ = reader.Close()
		_ = writer.Close()
		_ = root.Close()

		return nil, openError(err)
	}

	if err := validateAuxiliary(root); err != nil {
		_ = reader.Close()
		_ = writer.Close()
		_ = root.Close()

		return nil, openError(err)
	}

	lifetime, cancel := context.WithCancel(context.Background())

	return &Store{writer: writer, reader: reader, root: root, connectionLimits: limits,
		lifetime: lifetime, cancel: cancel, closed: make(chan struct{})}, nil
}

func openError(err error) error {
	// Only safe fixed recovery guidance and classified local errors are emitted.
	return fmt.Errorf("open private SQLite search state: %w; stop all writers, back up search and canonical state, "+
		"then rebuild only disposable search state", err)
}

func poolURI(path string, readonly, inspect bool) string {
	u := url.URL{Scheme: "file", Path: path}
	query := url.Values{}
	query.Set("mode", "rw") // The private database was exclusively created already.

	if inspect {
		query.Set("mode", "ro")
	} else {
		for _, pragma := range []string{
			"journal_mode(WAL)", "synchronous(FULL)", "foreign_keys(ON)", "busy_timeout(1000)", "temp_store(MEMORY)",
		} {
			query.Add("_pragma", pragma)
		}
	}

	if readonly {
		query.Add("_pragma", "query_only(ON)")
	}

	u.RawQuery = query.Encode()

	return u.String()
}

func openPool(ctx context.Context, path string, readonly bool, size int) (*sql.DB, error) {
	db, err := sql.Open("sqlite", poolURI(path, readonly, false))
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(size)
	db.SetMaxIdleConns(size)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()

		return nil, err
	}

	return db, nil
}

//nolint:cyclop // Read-only integrity gates classify failures without exposing database payloads.
func inspectExisting(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", poolURI(path, true, true))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	db.SetMaxOpenConns(1)

	var appID, version int
	if err := db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&appID); err != nil {
		return fmt.Errorf("%w: application header", ErrCorrupt)
	}

	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("%w: schema header", ErrCorrupt)
	}

	if appID != applicationID || version != schemaVersion {
		return ErrSchema
	}

	var check string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&check); err != nil || check != "ok" {
		return fmt.Errorf("%w: integrity check", ErrCorrupt)
	}

	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("%w: foreign keys", ErrCorrupt)
	}

	bad := rows.Next()
	rowErr := rows.Err()
	_ = rows.Close()

	if bad || rowErr != nil {
		return fmt.Errorf("%w: foreign keys", ErrCorrupt)
	}

	return inspectLexical(ctx, db)
}

func inspectLexical(ctx context.Context, db *sql.DB) error {
	// Preparing the fixed shape also rejects missing required tables/columns.
	for _, statement := range []string{
		`SELECT b.physical_build_id,b.scope_kind,b.scope_key,b.generation_id,b.projection_id,
		 b.schema_version,b.preprocessing_version,b.watermark,b.state,b.record_count,
		 b.source_count,b.conversation_count,b.revoked FROM lexical_build b LIMIT 0`,
		`SELECT a.scope_kind,a.scope_key,a.physical_build_id,p.physical_build_id
		 FROM lexical_active a LEFT JOIN lexical_previous p
		 ON p.scope_kind=a.scope_kind AND p.scope_key=a.scope_key LIMIT 0`,
		`SELECT r.rowid,r.item_id,r.revision_id,r.revision,r.change_seq,r.kind,r.category,r.memory_key,
		 r.created_at,r.observed_at,r.valid_from,r.valid_until,r.expires_at,r.sensitivity,r.retention,
		 r.raw_tokens,r.ru_tokens,r.en_tokens,s.id,c.id FROM lexical_record r
		 LEFT JOIN lexical_source s ON s.record_rowid=r.rowid
		 LEFT JOIN lexical_conversation c ON c.record_rowid=r.rowid LIMIT 0`,
	} {
		rows, err := db.QueryContext(ctx, statement)
		if err != nil {
			return fmt.Errorf("%w: lexical schema", ErrCorrupt)
		}

		_ = rows.Close()
	}

	var orphan bool

	err := db.QueryRowContext(ctx, `SELECT EXISTS(
	 SELECT 1 FROM lexical_fts f LEFT JOIN lexical_record r ON r.rowid=f.rowid WHERE r.rowid IS NULL)
	 OR EXISTS(SELECT 1 FROM lexical_record r LEFT JOIN lexical_fts f
	 ON f.rowid=r.rowid WHERE f.rowid IS NULL)`).Scan(&orphan)
	if err != nil || orphan {
		return fmt.Errorf("%w: lexical FTS ownership", ErrCorrupt)
	}

	return nil
}
