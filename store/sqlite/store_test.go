package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/sqlite"
)

func TestPublicOwnerInvalidAndCanceled(t *testing.T) {
	if store, err := sqlite.Open(t.Context(), sqlite.Config{}); store != nil || err == nil {
		t.Fatal("empty path admitted")
	}

	var store *sqlite.Store
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	assertUnavailable(t, store.CheckIntegrity(t.Context()))
	assertUnavailable(t, sqlite.NewCanonicalReader(nil).CheckIntegrity(t.Context()))

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
