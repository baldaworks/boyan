//go:build integration

package canonicalsql

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func openTestOwner(t *testing.T, path string) *Owner {
	t.Helper()

	owner, err := OpenSQLiteOwner(t.Context(), SQLiteConfig{Path: path})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := owner.Close(); err != nil {
			t.Error(err)
		}
	})

	return owner
}

// Fails if acknowledgements do not survive reopening or any dedicated
// connection can omit WAL/FULL, foreign keys, or CHECK enforcement.
func TestSQLiteOwnerDurability(t *testing.T) {
	path := filepath.Join(t.TempDir(), "canonical.sqlite")
	key := Key{Family: Payload, Parts: [][]byte{[]byte("durable")}}
	value := envelope(t, Payload, "committed")

	for round := range 2 {
		owner := openTestOwner(t, path)
		assertSQLitePragmas(t, owner)

		if round == 0 {
			if err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
				return tx.Put(ctx, key, value)
			}); err != nil {
				t.Fatal(err)
			}
		} else if err := owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
			got, err := tx.Get(ctx, key)
			if !bytes.Equal(got, value) {
				t.Error("committed row did not survive reopen")
			}

			return err
		}); err != nil {
			t.Fatal(err)
		}

		if err := owner.Close(); err != nil {
			t.Fatal(err)
		}
	}

	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("canonical file permissions: %v %v", info, err)
	}
}

func assertSQLitePragmas(t *testing.T, owner *Owner) {
	t.Helper()

	for pragma, want := range map[string]string{
		"journal_mode": "wal", "synchronous": "2", "foreign_keys": "1", "ignore_check_constraints": "0",
		"busy_timeout": "1000",
	} {
		var got string
		if err := scanOne(t.Context(), owner.connection.q, "PRAGMA "+pragma, nil, &got); err != nil || got != want {
			t.Fatalf("%s=%s, want %s: %v", pragma, got, want, err)
		}
	}
}

// A different path spelling, symlink, or hard link must not create a second
// supported owner for the same file. Locking only a path sidecar misses aliases.
func TestSQLiteOwnerExcludesAliases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "canonical.sqlite")
	owner := openTestOwner(t, path)
	symlink := filepath.Join(dir, "symbolic.sqlite")
	hardlink := filepath.Join(dir, "hard.sqlite")

	if err := os.Symlink(path, symlink); err != nil {
		t.Fatal(err)
	}

	if err := os.Link(path, hardlink); err != nil {
		t.Fatal(err)
	}

	for _, alias := range []string{path, symlink, hardlink} {
		second, err := OpenSQLiteOwner(t.Context(), SQLiteConfig{Path: alias})
		if second != nil {
			_ = second.Close()

			t.Fatal("second owner admitted")
		}

		assertCode(t, err, memory.CodeUnavailable)
	}

	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}

	openTestOwner(t, path)
}

// Invalid/canceled opens must not consume ownership or interpret paths as DSNs.
func TestSQLiteOwnerRejectsInvalidOpen(t *testing.T) {
	for _, config := range []SQLiteConfig{{}, {Path: ":memory:"}, {Path: "file:private?mode=memory"},
		{Path: filepath.Join(t.TempDir(), "canonical.sqlite"), BusyTimeout: -time.Second},
		{Path: filepath.Join(t.TempDir(), "canonical.sqlite"), BusyTimeout: time.Nanosecond},
		{Path: filepath.Join(t.TempDir(), "canonical.sqlite"), BusyTimeout: 6 * time.Second}} {
		if owner, err := OpenSQLiteOwner(t.Context(), config); err == nil || owner != nil {
			if owner != nil {
				_ = owner.Close()
			}

			t.Fatal("invalid configuration admitted")
		}
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	path := filepath.Join(t.TempDir(), "canonical.sqlite")
	if owner, err := OpenSQLiteOwner(ctx, SQLiteConfig{Path: path}); owner != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled open: %v", err)
	}

	openTestOwner(t, path)
}
