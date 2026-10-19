// Package postgres owns a PostgreSQL canonical database through one pinned
// session. Domain ports are added as their shared SQL algorithms become
// available; the local SQLite search projection remains separate.
package postgres

import (
	"context"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsql"
)

// Config supplies a private resolved pgx connection string. Remote connections
// require sslmode=verify-full. Local Unix/loopback sslmode=disable additionally
// requires AllowLocalInsecure. Multiple hosts/downgrade fallbacks are rejected.
// Zero ConnectTimeout selects five seconds; the maximum is thirty seconds.
type Config struct {
	DSN                string
	ConnectTimeout     time.Duration
	AllowLocalInsecure bool
}

// String redacts connection configuration in ordinary diagnostic formatting.
func (Config) String() string { return "canonical PostgreSQL config (redacted)" }

// GoString redacts connection configuration in %#v diagnostic formatting.
func (config Config) GoString() string { return config.String() }

// Validate checks connection syntax, TLS policy and timeout without connecting.
func (config Config) Validate() error {
	return (canonicalsql.PostgresConfig{DSN: config.DSN, ConnectTimeout: config.ConnectTimeout,
		AllowLocalInsecure: config.AllowLocalInsecure}).Validate()
}

// Store is the stable comparable authority for this database. It owns exactly
// one direct connection and cannot reconnect after losing session ownership.
type Store struct{ owner *canonicalsql.Owner }

// Open acquires fixed session ownership before migration. Persisted loading
// control can remain open for later recovery, but RestorePreflight must pass
// before application readiness. No other database backend is used on failure.
func Open(ctx context.Context, config Config) (*Store, error) {
	owner, err := canonicalsql.OpenPostgresOwner(ctx, canonicalsql.PostgresConfig{
		DSN: config.DSN, ConnectTimeout: config.ConnectTimeout, AllowLocalInsecure: config.AllowLocalInsecure,
	})
	if err != nil {
		return nil, err
	}

	return &Store{owner: owner}, nil
}

// RestorePreflight checks session ownership, durable loading control and stored
// integrity. A disconnected/poisoned owner requires full Close/Open recovery.
func (s *Store) RestorePreflight(ctx context.Context) error {
	return s.canonicalOwner().RestorePreflight(ctx)
}

// CheckReadiness verifies the pinned session, ownership and durable serving
// fences. A lost session poisons the owner; it is never reconnected here.
func (s *Store) CheckReadiness(ctx context.Context) error {
	return s.canonicalOwner().SourcePrivacyReady(ctx)
}

// Close rejects new work, cancels/drains admitted work and closes the session,
// releasing its lock. Repeated and concurrent calls share the cleanup result.
func (s *Store) Close() error { return s.canonicalOwner().Close() }

func (s *Store) canonicalOwner() *canonicalsql.Owner {
	if s == nil {
		return nil
	}

	return s.owner
}

// Reader shares its exact Store authority and lifecycle.
type Reader struct{ store *Store }

// NewCanonicalReader binds a reader to the store's pinned canonical authority.
func NewCanonicalReader(store *Store) *Reader { return &Reader{store: store} }

// RestorePreflight checks the same readiness boundary as the owning Store.
func (r *Reader) RestorePreflight(ctx context.Context) error {
	if r == nil {
		return (*Store)(nil).RestorePreflight(ctx)
	}

	return r.store.RestorePreflight(ctx)
}
