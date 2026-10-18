package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
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
	if err := reader.RestorePreflight(t.Context()); err != nil {
		t.Fatal(err)
	}

	raw, err := canonicalsqltest.OpenSQLite(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := raw.SQLite.ExecContext(t.Context(),
		"INSERT INTO restore_control(singleton,control) VALUES(1,$1)", []byte("loading")); err != nil {
		t.Fatal(err)
	}

	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	assertUnavailable(t, store.RestorePreflight(t.Context()))
	assertUnavailable(t, reader.RestorePreflight(t.Context()))

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	assertUnavailable(t, reader.RestorePreflight(t.Context()))

	reopened, err := sqlite.Open(t.Context(), sqlite.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = reopened.Close() }()

	assertUnavailable(t, reopened.RestorePreflight(t.Context()))
}

func TestPublicOwnerInvalidAndCanceled(t *testing.T) {
	if store, err := sqlite.Open(t.Context(), sqlite.Config{}); store != nil || err == nil {
		t.Fatal("empty path admitted")
	}

	var store *sqlite.Store
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	assertUnavailable(t, store.RestorePreflight(t.Context()))
	assertUnavailable(t, sqlite.NewCanonicalReader(nil).RestorePreflight(t.Context()))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	config := sqlite.Config{Path: filepath.Join(t.TempDir(), "canonical.sqlite")}
	if _, err := sqlite.Open(ctx, config); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled open: %v", err)
	}
}

func assertUnavailable(t *testing.T, err error) {
	t.Helper()

	code, _, ok := memory.ClassifyError(err)
	if !ok || code != memory.CodeUnavailable {
		t.Fatalf("expected unavailable, got %v", err)
	}
}
