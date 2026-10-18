// Package sqlite owns an exclusive, durable SQLite canonical file. The search
// projection is a separate database owned by index/sqlite. Domain ports are
// added as their shared canonical SQL implementation becomes available.
package sqlite

import (
	"context"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsql"
)

// Config selects a canonical file. Path must be an ordinary filesystem path;
// SQLite URIs and in-memory stores are unsupported. Zero BusyTimeout selects
// one second; explicit values must be between one millisecond and five seconds.
type Config struct {
	Path        string
	BusyTimeout time.Duration
}

// Store is the stable comparable canonical authority for this file. It holds
// one dedicated connection; it never reconnects or switches canonical backends.
type Store struct{ owner *canonicalsql.Owner }

// Open acquires exclusive ownership and migrates compatible SQL state. A store
// with interrupted restore control can open for later controlled recovery;
// RestorePreflight must succeed before application readiness.
func Open(ctx context.Context, config Config) (*Store, error) {
	owner, err := canonicalsql.OpenSQLiteOwner(ctx, canonicalsql.SQLiteConfig{
		Path: config.Path, BusyTimeout: config.BusyTimeout,
	})
	if err != nil {
		return nil, err
	}

	return &Store{owner: owner}, nil
}

// RestorePreflight rejects interrupted or incomplete restores before serving.
func (s *Store) RestorePreflight(ctx context.Context) error {
	return s.canonicalOwner().RestorePreflight(ctx)
}

// Close rejects admissions, cancels/drains active operations, checkpoints WAL
// and releases ownership. It is safe to call repeatedly or concurrently.
func (s *Store) Close() error { return s.canonicalOwner().Close() }

func (s *Store) canonicalOwner() *canonicalsql.Owner {
	if s == nil {
		return nil
	}

	return s.owner
}

// Reader shares its store's exact authority and lifecycle. No memory reader
// port is claimed until the corresponding domain operations are implemented.
type Reader struct{ store *Store }

// NewCanonicalReader creates a reader bound to the same canonical owner.
func NewCanonicalReader(store *Store) *Reader { return &Reader{store: store} }

// RestorePreflight checks the reader's authority before application startup.
func (r *Reader) RestorePreflight(ctx context.Context) error {
	if r == nil {
		return (*Store)(nil).RestorePreflight(ctx)
	}

	return r.store.RestorePreflight(ctx)
}
