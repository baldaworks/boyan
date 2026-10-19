package canonicalsql

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	defaultConnectTimeout = 5 * time.Second
	maxConnectTimeout     = 30 * time.Second
	// A fixed two-int namespace identifies canonical ownership in this database.
	postgresLockClass  = 1651472750
	postgresLockObject = 1667329647
)

// PostgresConfig contains resolved private connection configuration. Remote
// connections require sslmode=verify-full. Explicit sslmode=disable is allowed
// only with AllowLocalInsecure and a Unix socket or loopback destination.
// ConnectTimeout defaults to five seconds and is bounded to thirty seconds.
type PostgresConfig struct {
	DSN                string
	ConnectTimeout     time.Duration
	AllowLocalInsecure bool
}

// String prevents accidental credential disclosure through config formatting.
func (PostgresConfig) String() string { return "canonical PostgreSQL config (redacted)" }

// GoString also redacts diagnostic %#v formatting.
func (config PostgresConfig) GoString() string { return config.String() }

// Validate checks the same native parser/TLS policy as OpenPostgresOwner,
// without dialing, acquiring ownership or opening a database.
func (config PostgresConfig) Validate() error {
	_, err := config.parse()

	return err
}

// OpenPostgresOwner acquires session ownership before migration on a single
// direct connection. It never reconnects, retries commits, or falls back to
// another database backend.
func OpenPostgresOwner(ctx context.Context, config PostgresConfig) (*Owner, error) {
	if ctx == nil {
		return nil, invalid("canonical PostgreSQL context is required")
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	nativeConfig, err := config.parse()
	if err != nil {
		return nil, err
	}

	connectCtx, cancel := context.WithTimeout(ctx, nativeConfig.ConnectTimeout)
	defer cancel()

	native, err := pgx.ConnectConfig(connectCtx, nativeConfig)
	if err != nil {
		return nil, databaseError(connectCtx, "connect PostgreSQL", err)
	}

	c := NewPostgres(native)

	closeNative := func() error {
		cleanup, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
		defer cancel()

		return databaseError(cleanup, "close PostgreSQL", native.Close(cleanup))
	}
	if err := acquirePostgresOwnership(ctx, c); err != nil {
		_ = closeNative()

		return nil, err
	}

	if err := c.Migrate(ctx); err != nil {
		_ = closeNative()

		return nil, err
	}

	return newOwner(c, closeNative), nil
}

func (config PostgresConfig) parse() (*pgx.ConnConfig, error) {
	if strings.TrimSpace(config.DSN) == "" || config.ConnectTimeout < 0 || config.ConnectTimeout > maxConnectTimeout {
		return nil, invalid("invalid canonical PostgreSQL configuration")
	}

	native, err := pgx.ParseConfig(config.DSN)
	if err != nil {
		return nil, invalid("invalid canonical PostgreSQL connection configuration")
	}
	// Multiple hosts and downgrade fallbacks expand the identity/ownership
	// boundary. This owner supports a single explicitly secured destination.
	if len(native.Fallbacks) != 0 {
		return nil, invalid("canonical PostgreSQL connection fallbacks are unsupported")
	}

	if err := config.secure(native); err != nil {
		return nil, err
	}

	native.ConnectTimeout = config.ConnectTimeout
	if native.ConnectTimeout == 0 {
		native.ConnectTimeout = defaultConnectTimeout
	}
	// Canonical tables are explicitly qualified. Keep type, operator and
	// function resolution independent of caller-controlled search_path too.
	native.RuntimeParams["search_path"] = "pg_catalog"

	return native, nil
}

func (config PostgresConfig) secure(native *pgx.ConnConfig) error {
	if native.TLSConfig != nil {
		if native.TLSConfig.InsecureSkipVerify || native.TLSConfig.ServerName == "" {
			return invalid("canonical PostgreSQL requires verified TLS identity")
		}

		return nil
	}

	if !config.AllowLocalInsecure {
		return invalid("canonical PostgreSQL local TLS disable must be explicit")
	}

	if filepath.IsAbs(native.Host) {
		return nil
	}

	if ip := net.ParseIP(native.Host); ip != nil && ip.IsLoopback() {
		return nil
	}

	if native.Host != "localhost" {
		return invalid("canonical PostgreSQL remote TLS disable is forbidden")
	}
	// Even a misconfigured resolver must not send plaintext credentials to a
	// remote address under the localhost spelling.
	native.LookupFunc = func(context.Context, string) ([]string, error) {
		return []string{"127.0.0.1", "::1"}, nil
	}

	return nil
}

func acquirePostgresOwnership(ctx context.Context, c *Connection) error {
	var locked bool

	err := scanOne(ctx, c.q, "SELECT pg_catalog.pg_try_advisory_lock($1::integer,$2::integer)",
		[]any{postgresLockClass, postgresLockObject}, &locked)
	if err != nil {
		return databaseError(ctx, "acquire PostgreSQL ownership", err)
	}

	if !locked {
		return ownerUnavailable()
	}

	return nil
}

func (o *Owner) checkPostgresOwnership(ctx context.Context) error {
	if o.connection.pg == nil {
		return nil
	}

	var held bool

	err := scanOne(ctx, o.connection.q, `SELECT EXISTS (
		SELECT 1 FROM pg_catalog.pg_locks
		WHERE locktype='advisory' AND pid=pg_catalog.pg_backend_pid()
		AND classid=$1::oid AND objid=$2::oid AND objsubid=2 AND granted
		AND mode='ExclusiveLock')`, []any{postgresLockClass, postgresLockObject}, &held)
	if err != nil || !held {
		o.poison()

		if ctx.Err() != nil {
			return ctx.Err()
		}

		return ownerUnavailable()
	}

	return nil
}

// Called while the admission gate is held, after transaction rollback. pgx
// closes the socket on protocol failures; non-idle/busy state also means the
// session's transaction/ownership outcome cannot safely be reused.
func (o *Owner) fenceBrokenPostgres() {
	native := o.connection.pg
	if native != nil && (native.IsClosed() || native.PgConn().IsBusy() || native.PgConn().TxStatus() != 'I') {
		o.poison()
	}
}

func (o *Owner) poison() {
	o.mu.Lock()
	o.poisoned = true
	o.mu.Unlock()
}
