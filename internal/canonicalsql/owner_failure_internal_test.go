package canonicalsql

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// A failed driver/configuration stage must preserve preexisting file bytes and
// close both the pinned connection and inode lock, just like migration failure.
func TestSQLiteOwnerRejectsForeignFileWithoutTruncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unrelated?mode=memory.sqlite")

	original := []byte("unrelated existing private state\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		owner, err := OpenSQLiteOwner(t.Context(), SQLiteConfig{Path: path})
		if owner != nil {
			_ = owner.Close()

			t.Fatal("foreign bytes accepted as canonical database")
		}

		if err == nil || strings.Contains(err.Error(), "unrelated") {
			t.Fatalf("unclassified or unsanitized open error: %v", err)
		}

		assertNoSQLiteHandles(t, path)
	}

	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatalf("failed opening changed existing file bytes: %v", err)
	}
}

// Ordinary admission must inspect persistent restore control before invoking
// callbacks, including when a new owner opens an interrupted loading store.
// Native contention must be bounded, cancellation must preserve context
// identity, and neither BEGIN failure may poison the next admitted transaction.
func TestSQLiteOwnerBusyAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "canonical.sqlite")

	owner, err := OpenSQLiteOwner(t.Context(), SQLiteConfig{Path: path, BusyTimeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = owner.Close() })
	raw := openSQLiteBlocker(t, path)

	called := false
	callback := func(context.Context, *Tx) error {
		called = true

		return nil
	}
	started := time.Now()
	err = owner.Update(t.Context(), callback)
	assertCode(t, err, memory.CodeConflict)

	if !memory.IsRetryable(err) || called || time.Since(started) > 2*time.Second {
		t.Fatalf("unbounded or unclassified native contention: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	if err := owner.Update(ctx, callback); !errors.Is(err, context.DeadlineExceeded) || called {
		t.Fatalf("busy admission lost cancellation: %v", err)
	}

	if _, err := raw.SQLite.ExecContext(t.Context(), "ROLLBACK"); err != nil {
		t.Fatal(err)
	}

	if err := owner.Update(t.Context(), callback); err != nil || !called {
		t.Fatalf("failed BEGIN contaminated pinned connection: %v", err)
	}
}

func openSQLiteBlocker(t *testing.T, path string) *canonicalsqltest.Database {
	t.Helper()

	raw, err := canonicalsqltest.OpenSQLite(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = raw.Close() })

	if _, err := raw.SQLite.ExecContext(t.Context(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}

	return raw
}

// A checkpoint that returns SQLite's busy row without a driver error must not
// be acknowledged as successful lifecycle maintenance. Close still frees the
// connection and file lock so the supported owner can reopen durable WAL.
func TestSQLiteOwnerCloseReportsBlockedCheckpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "canonical.sqlite")

	owner, err := OpenSQLiteOwner(t.Context(), SQLiteConfig{Path: path, BusyTimeout: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = owner.Close() })

	raw, err := canonicalsqltest.OpenSQLite(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = raw.Close() })

	if _, err := raw.SQLite.ExecContext(t.Context(), "BEGIN"); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := raw.SQLite.QueryRowContext(t.Context(), "SELECT count(*) FROM source").Scan(&count); err != nil {
		t.Fatal(err)
	}

	key := Key{Family: Payload, Parts: [][]byte{[]byte("checkpoint")}}

	err = owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		return tx.Put(ctx, key, envelope(t, Payload, "durable WAL"))
	})
	if err != nil {
		t.Fatal(err)
	}

	assertCode(t, owner.Close(), memory.CodeConflict)

	if _, err := raw.SQLite.ExecContext(t.Context(), "ROLLBACK"); err != nil {
		t.Fatal(err)
	}

	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openTestOwner(t, path)
	if err := reopened.View(t.Context(), func(ctx context.Context, tx *Tx) error {
		got, err := tx.Get(ctx, key)
		if !bytes.Equal(got, envelope(t, Payload, "durable WAL")) {
			t.Fatal("checkpoint failure lost committed payload")
		}

		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// Unsupported schema must close every native resource and release ownership,
// without resetting the schema or clearing a durable recovery marker.
func TestSQLiteOwnerPartialOpenCleanup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "canonical.sqlite")
	owner := openTestOwner(t, path)
	rawExec(t, owner.connection, "UPDATE schema_migration SET checksum='unsupported'")
	rawExec(t, owner.connection, "CREATE TABLE prior_state(value TEXT)")
	rawExec(t, owner.connection, "INSERT INTO prior_state VALUES('preserved')")

	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}

	for range 3 {
		failed, err := OpenSQLiteOwner(t.Context(), SQLiteConfig{Path: path})
		if failed != nil {
			_ = failed.Close()

			t.Fatal("unsupported schema admitted")
		}

		assertCode(t, err, memory.CodeInvalidDerived)
		assertNoSQLiteHandles(t, path)
	}

	raw, err := canonicalsqltest.OpenSQLite(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = raw.Close() }()

	var checksum, control string

	row := raw.SQLite.QueryRowContext(t.Context(), "SELECT checksum FROM schema_migration")
	if err := row.Scan(&checksum); err != nil {
		t.Fatal(err)
	}

	if err := raw.SQLite.QueryRowContext(t.Context(), "SELECT value FROM prior_state").Scan(&control); err != nil {
		t.Fatal(err)
	}

	if checksum != "unsupported" || control != "preserved" {
		t.Fatal("failed open mutated schema history or prior state")
	}
}

func assertNoSQLiteHandles(t *testing.T, path string) {
	t.Helper()

	entries, err := os.ReadDir("/proc/self/fd")
	if errors.Is(err, os.ErrNotExist) {
		return // Ownership/repeated-open assertions above remain portable.
	}

	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		target, _ := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if target == path || strings.HasPrefix(target, path+"-") {
			t.Fatalf("partial open leaked SQLite handle %s", entry.Name())
		}
	}
}
