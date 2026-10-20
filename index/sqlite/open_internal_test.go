//go:build integration

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func openTestStore(t *testing.T, enabled bool) (*Store, string) {
	t.Helper()

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	store, err := Open(t.Context(), dir, Options{EmbeddingsEnabled: enabled})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})

	return store, dir
}

func TestFreshLexicalOnlyReopenAndPrivateFiles(t *testing.T) {
	t.Parallel()
	store, dir := openTestStore(t, false)

	var semanticTables int

	const semanticCount = `SELECT count(*) FROM sqlite_schema WHERE name LIKE 'semantic_%'`
	if err := store.reader.QueryRowContext(t.Context(), semanticCount).Scan(&semanticTables); err != nil {
		t.Fatal(err)
	}

	if semanticTables != 0 {
		t.Fatal("disabled database has semantic tables")
	}

	for _, name := range []string{
		"search", "search/search.sqlite", "search/search.sqlite-wal", "search/search.sqlite-shm",
	} {
		info, err := os.Lstat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}

		want := os.FileMode(0o600)
		if info.IsDir() {
			want = 0o700
		}

		if info.Mode().Perm() != want {
			t.Fatalf("%s permissions %o", name, info.Mode().Perm())
		}
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(t.Context(), dir, Options{})
	if err != nil {
		t.Fatal(err)
	}

	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestReopenWhileWALLive(t *testing.T) {
	t.Parallel()
	_, dir := openTestStore(t, false)

	reopened, err := Open(t.Context(), dir, Options{})
	if err != nil {
		t.Fatal(err)
	}

	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

//nolint:cyclop,gocognit // Check all settings on all concurrently held physical connections.
func TestPhysicalPoolsEnforceSettingsAndReadOnly(t *testing.T) {
	t.Parallel()

	store, _ := openTestStore(t, true)
	for _, pool := range []struct {
		db         *sql.DB
		size, only int
	}{{store.writer, 1, 0}, {store.reader, 2, 1}} {
		if pool.db.Stats().MaxOpenConnections != pool.size {
			t.Fatal("pool size")
		}

		connections := make([]*sql.Conn, 0, pool.size)
		for range pool.size {
			conn, err := pool.db.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			connections = append(connections, conn)
		}

		for _, conn := range connections {
			for _, setting := range []struct {
				name string
				want int
			}{{"synchronous", 2}, {"foreign_keys", 1}, {"busy_timeout", 1000}, {"temp_store", 2}, {"query_only", pool.only}} {
				var got int
				if err := conn.QueryRowContext(t.Context(), "PRAGMA "+setting.name).Scan(&got); err != nil {
					t.Fatal(err)
				}

				if got != setting.want {
					t.Fatalf("%s=%d", setting.name, got)
				}
			}

			var mode, vec string
			if err := conn.QueryRowContext(t.Context(), "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
				t.Fatalf("WAL %s %v", mode, err)
			}

			if err := conn.QueryRowContext(t.Context(), "SELECT vec_version()").Scan(&vec); err != nil || vec != "v0.1.9" {
				t.Fatalf("vec %s %v", vec, err)
			}

			if pool.only == 1 {
				if _, err := conn.ExecContext(t.Context(), "DELETE FROM lexical_build"); err == nil {
					t.Fatal("reader wrote")
				}
			}
		}

		for _, conn := range connections {
			if err := conn.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestCloseCancelsSnapshotsDrainsAndRejectsNewWork(t *testing.T) {
	t.Parallel()
	store, _ := openTestStore(t, false)

	snapshot, err := store.read(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	if err := store.CloseContext(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := snapshot.tx.ExecContext(t.Context(), "SELECT 1"); err == nil {
		t.Fatal("snapshot survived close")
	}

	if _, err := store.read(t.Context()); !errors.Is(err, ErrClosed) {
		t.Fatalf("read after close: %v", err)
	}

	err = store.write(context.Background(), func(context.Context, *sql.Tx) error { return nil })
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("write after close: %v", err)
	}

	if err := snapshot.Close(); err != nil {
		t.Fatal(err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}
