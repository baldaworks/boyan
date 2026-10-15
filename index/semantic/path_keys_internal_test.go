package semantic

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dgraph-io/badger/v4"
)

func TestOpenRejectsSymlinkedBadgerChildWithoutChangingTarget(t *testing.T) {
	base := t.TempDir()
	directory := filepath.Join(base, "embeddings")
	internalRequireNoError(t, os.Mkdir(directory, 0o700))

	target := filepath.Join(base, "target-manifest")
	want := []byte("target contents must remain untouched")
	internalRequireNoError(t, os.WriteFile(target, want, 0o640))
	internalRequireNoError(t, os.Symlink(target, filepath.Join(directory, "MANIFEST")))

	store, err := Open(directory)
	if store != nil {
		_ = store.Close()
	}

	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Open(symlinked MANIFEST) error = %v, want ErrInvalid", err)
	}

	got, readErr := os.ReadFile(target)
	internalRequireNoError(t, readErr)

	if !bytes.Equal(got, want) {
		t.Fatalf("symlink target contents = %q, want %q", got, want)
	}

	info, statErr := os.Stat(target)
	internalRequireNoError(t, statErr)

	if gotMode := info.Mode().Perm(); gotMode != 0o640 {
		t.Fatalf("symlink target mode = %o, want unchanged 640", gotMode)
	}
}

func TestOpenRejectsUnsafeSpecialBadgerChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-domain socket fixture is not supported on Windows")
	}

	directory := filepath.Join(t.TempDir(), "embeddings")
	internalRequireNoError(t, os.Mkdir(directory, 0o700))

	listener, err := net.Listen("unix", filepath.Join(directory, "unsafe.sock"))
	if err != nil {
		t.Skipf("Unix-domain socket fixture is unavailable: %v", err)
	}

	t.Cleanup(func() { internalRequireNoError(t, listener.Close()) })

	store, err := Open(directory)
	if store != nil {
		_ = store.Close()
	}

	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Open(database tree with socket) error = %v, want ErrInvalid", err)
	}
}

func TestOpenRejectsSymlinkedRootWithoutChangingTarget(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	internalRequireNoError(t, os.Mkdir(target, 0o750))

	link := filepath.Join(base, "embeddings")
	internalRequireNoError(t, os.Symlink(target, link))

	store, err := Open(link)
	if store != nil {
		_ = store.Close()
	}

	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Open(symlink root) error = %v, want ErrInvalid", err)
	}

	info, statErr := os.Stat(target)
	internalRequireNoError(t, statErr)

	if got := info.Mode().Perm(); got != 0o750 {
		t.Fatalf("symlink target mode = %o, want unchanged 750", got)
	}
}

func TestOpenRejectsSymlinkedParentWithoutCreatingTargetChild(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	internalRequireNoError(t, os.Mkdir(target, 0o750))

	link := filepath.Join(base, "state")
	internalRequireNoError(t, os.Symlink(target, link))

	store, err := Open(filepath.Join(link, "embeddings"))
	if store != nil {
		_ = store.Close()
	}

	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Open(symlink parent) error = %v, want ErrInvalid", err)
	}

	if _, statErr := os.Lstat(filepath.Join(target, "embeddings")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("symlink target child exists or stat failed: %v", statErr)
	}
}

func TestSnapshotRejectsChunkKeyIndexMismatch(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

	generation, coverage, chunks := internalFixture("key-index", "bleve-1", 1)
	_, err = store.Stage(context.Background(), generation, coverage, chunks)
	internalRequireNoError(t, err)
	internalRequireNoError(t, store.Publish(context.Background(), Publication{
		View: generation.View(), BuildID: generation.BuildID,
	}))

	internalRequireNoError(t, store.db.Update(func(transaction *badger.Txn) error {
		item, getErr := transaction.Get(chunkKey(generation.Scope, generation.BuildID, 0))
		if getErr != nil {
			return getErr
		}

		value, readErr := readItemValueBounded(item, maxChunkValueBytes)
		if readErr != nil {
			return readErr
		}

		if setErr := transaction.Set(chunkKey(generation.Scope, generation.BuildID, 1), value); setErr != nil {
			return setErr
		}

		return transaction.Delete(chunkKey(generation.Scope, generation.BuildID, 0))
	}))

	if _, _, err := store.Snapshot(context.Background(), generation.View()); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Snapshot(mismatched chunk key index) error = %v, want ErrCorrupt", err)
	}
}

func TestGarbageCollectionDeletesMalformedActiveChunkKeys(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "embeddings"))
	internalRequireNoError(t, err)
	t.Cleanup(func() { internalRequireNoError(t, store.Close()) })

	generation, coverage, chunks := internalFixture("malformed-key", "bleve-1", 1)
	_, err = store.Stage(context.Background(), generation, coverage, chunks)
	internalRequireNoError(t, err)
	internalRequireNoError(t, store.Publish(context.Background(), Publication{
		View: generation.View(), BuildID: generation.BuildID,
	}))

	valid := chunkKey(generation.Scope, generation.BuildID, 0)
	malformed := append(append([]byte(nil), valid...), 0xff)

	internalRequireNoError(t, store.db.Update(func(transaction *badger.Txn) error {
		return transaction.Set(malformed, []byte("malformed"))
	}))
	internalRequireNoError(t, store.CollectGarbage(context.Background()))

	internalRequireNoError(t, store.db.View(func(transaction *badger.Txn) error {
		if _, getErr := transaction.Get(valid); getErr != nil {
			t.Fatalf("valid active chunk removed: %v", getErr)
		}

		if _, getErr := transaction.Get(malformed); !errors.Is(getErr, badger.ErrKeyNotFound) {
			t.Fatalf("malformed active-prefix key still exists: %v", getErr)
		}

		return nil
	}))
}
