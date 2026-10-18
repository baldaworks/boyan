// Package storefixture runs cases through implemented public canonical owners.
// It is separate from the raw fixture package to avoid primitive-test cycles.
package storefixture

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	"github.com/baldaworks/boyan/store/sqlite"
)

// Store is the implemented lifecycle shared by canonical owners. Canonical
// domain conformance belongs to the later tasks implementing those ports.
type Store interface {
	RestorePreflight(ctx context.Context) error
	Close() error
}

// Fixture supplies the real owner and its constructor for reopen/lock cases.
type Fixture struct {
	Backend canonicalsqltest.Backend
	Store   Store
	Open    func(context.Context) (Store, error)
}

// ForEach runs available public owner factories. Only SQLite is implemented
// here; TASK-004 must add PostgreSQL before any owner parity claim is possible.
func ForEach(t *testing.T, run func(*testing.T, *Fixture)) {
	t.Helper()

	t.Run(string(canonicalsqltest.SQLite), func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "canonical.sqlite")
		open := func(ctx context.Context) (Store, error) {
			store, err := sqlite.Open(ctx, sqlite.Config{Path: path})
			if err != nil {
				return nil, err
			}

			return store, nil
		}

		store, err := open(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() {
			if err := store.Close(); err != nil {
				t.Error(err)
			}
		})
		run(t, &Fixture{Backend: canonicalsqltest.SQLite, Store: store, Open: open})
	})
}
