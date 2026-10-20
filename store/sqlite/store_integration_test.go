//go:build integration

package sqlite_test

import (
	"path/filepath"
	"testing"

	"github.com/baldaworks/boyan/store/sqlite"
)

// Public readers must retain the exact owner and obey the same persisted fence
// and closed state. Opening a fenced file is reserved for later recovery.
func TestPublicOwnerAndReaderLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "canonical.sqlite")

	store, err := sqlite.Open(t.Context(), sqlite.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	reader := sqlite.NewCanonicalReader(store)
	if err := reader.CheckIntegrity(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	assertUnavailable(t, reader.CheckIntegrity(t.Context()))

	reopened, err := sqlite.Open(t.Context(), sqlite.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = reopened.Close() }()

	if err := reopened.CheckIntegrity(t.Context()); err != nil {
		t.Fatal(err)
	}
}
