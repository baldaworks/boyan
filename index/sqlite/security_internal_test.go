package sqlite

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

//nolint:cyclop,gocognit,funlen // Each hostile fixture has explicit setup and failure handling.
func TestUnsafePathsRejectedBeforeSQLite(t *testing.T) {
	t.Parallel()

	for _, scenario := range []string{
		"directory-mode", "directory-symlink", "db-mode", "db-symlink", "fifo", "wal-symlink",
		"shm-mode", "journal-fifo", "ancestor-symlink",
	} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chmod(dir, 0o700); err != nil {
				t.Fatal(err)
			}

			search := filepath.Join(dir, "search")
			if err := os.Mkdir(search, 0o700); err != nil {
				t.Fatal(err)
			}

			db := filepath.Join(search, "search.sqlite")

			switch scenario {
			case "directory-mode":
				if err := os.Chmod(search, 0o755); err != nil {
					t.Fatal(err)
				}
			case "directory-symlink":
				if err := os.Rename(search, search+"-real"); err != nil {
					t.Fatal(err)
				}

				if err := os.Symlink(search+"-real", search); err != nil {
					t.Fatal(err)
				}
			case "ancestor-symlink":
				linked := dir + "-link"
				if err := os.Symlink(dir, linked); err != nil {
					t.Fatal(err)
				}

				t.Cleanup(func() { _ = os.Remove(linked) })

				dir = linked
			case "db-mode":
				if err := os.WriteFile(db, []byte("original"), 0o644); err != nil {
					t.Fatal(err)
				}
			case "db-symlink", "wal-symlink":
				target := filepath.Join(dir, "target")
				if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
					t.Fatal(err)
				}

				name := db
				if scenario == "wal-symlink" {
					name += "-wal"
				}

				if err := os.Symlink(target, name); err != nil {
					t.Fatal(err)
				}
			case "fifo", "journal-fifo":
				name := db
				if scenario == "journal-fifo" {
					name += "-journal"
				}

				if err := syscall.Mkfifo(name, 0o600); err != nil {
					t.Fatal(err)
				}
			case "shm-mode":
				if err := os.WriteFile(db+"-shm", []byte("original"), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			store, err := Open(t.Context(), dir, Options{})
			if err == nil {
				_ = store.Close()

				t.Fatal("unsafe path accepted")
			}
		})
	}
}

//nolint:cyclop,gocognit // Preserve and compare each independently damaged fixture.
func TestCorruptAndUnknownDatabaseBytesRemainUnchanged(t *testing.T) {
	t.Parallel()

	for _, scenario := range []string{"corrupt", "application", "newer", "unknown"} {
		t.Run(scenario, func(t *testing.T) {
			store, dir := openTestStore(t, false)

			if scenario != "corrupt" {
				statement := "PRAGMA application_id=123"
				if scenario == "newer" {
					statement = "PRAGMA user_version=2"
				}

				if scenario == "unknown" {
					statement = "PRAGMA user_version=0"
				}

				execTest(t, store.writer, statement)
			}

			if err := store.Close(); err != nil {
				t.Fatal(err)
			}

			path := filepath.Join(dir, "search", "search.sqlite")
			if scenario == "corrupt" {
				if err := os.WriteFile(path, []byte("not a sqlite database"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			reopened, err := Open(t.Context(), dir, Options{})
			if err == nil {
				_ = reopened.Close()

				t.Fatal("invalid database accepted")
			}

			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			if sha256.Sum256(before) != sha256.Sum256(after) {
				t.Fatal("rejected database changed")
			}
		})
	}
}

func TestStateDirectoryCannotInjectURIOptions(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "state ?mode=memory&#fragment")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	store, err := Open(t.Context(), dir, Options{})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, "search", "search.sqlite")); err != nil {
		t.Fatal(err)
	}
}
