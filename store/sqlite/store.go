// Package sqlite owns an exclusive, durable SQLite canonical file. The search
// projection is a separate database owned by index/sqlite. Shared canonical SQL
// algorithms preserve temporal/history, replay and privacy memory contracts.
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

// Validate checks path syntax and timeout bounds without opening storage.
func (config Config) Validate() error {
	return (canonicalsql.SQLiteConfig{Path: config.Path, BusyTimeout: config.BusyTimeout}).Validate()
}

// Store is the stable comparable canonical authority for this file. It holds
// one dedicated connection; it never reconnects or switches canonical backends.
type Store struct{ owner *canonicalsql.Owner }

// Open acquires exclusive ownership and migrates compatible SQL state.
// CheckIntegrity must succeed before application readiness.
func Open(ctx context.Context, config Config) (*Store, error) {
	owner, err := canonicalsql.OpenSQLiteOwner(ctx, canonicalsql.SQLiteConfig{
		Path: config.Path, BusyTimeout: config.BusyTimeout,
	})
	if err != nil {
		return nil, err
	}

	return &Store{owner: owner}, nil
}

// CheckIntegrity validates structural canonical state before serving.
func (s *Store) CheckIntegrity(ctx context.Context) error {
	return s.canonicalOwner().CheckIntegrity(ctx)
}

// CheckReadiness verifies current ownership and durable serving fences without
// repeating startup's full integrity scan.
func (s *Store) CheckReadiness(ctx context.Context) error {
	return s.canonicalOwner().SourcePrivacyReady(ctx)
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

// Reader shares its store's exact authority and lifecycle for canonical reads.
type Reader struct{ store *Store }

// NewCanonicalReader creates a reader bound to the same canonical owner.
func NewCanonicalReader(store *Store) *Reader { return &Reader{store: store} }

// CheckIntegrity checks the reader's authority before application startup.
func (r *Reader) CheckIntegrity(ctx context.Context) error {
	if r == nil {
		return (*Store)(nil).CheckIntegrity(ctx)
	}

	return r.store.CheckIntegrity(ctx)
}
