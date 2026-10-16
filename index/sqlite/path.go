package sqlite

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	privateDirectoryMode = 0o700
	privateFileMode      = 0o600
	databaseHeaderBytes  = 100
)

// privateSearchRoot checks every existing ancestor without following symlinks.
// Root-relative inspection/creation confines our filesystem operations. SQLite
// itself opens a pathname: concurrent hostile changes by this same OS owner are
// outside the private state boundary. Callers must exclude external writers.
//
//nolint:cyclop,funlen // Keep no-symlink inspection and descriptor cleanup adjacent.
func privateSearchRoot(stateDir string) (*os.Root, string, bool, error) {
	abs, err := filepath.Abs(stateDir)
	if err != nil {
		return nil, "", false, err
	}

	root, err := os.OpenRoot(string(filepath.Separator))
	if err != nil {
		return nil, "", false, err
	}

	systemOwner, err := filesystemRootOwner(root)
	if err != nil {
		_ = root.Close()

		return nil, "", false, err
	}

	parts := strings.Split(strings.TrimPrefix(abs, string(filepath.Separator)), string(filepath.Separator))
	for index, part := range parts {
		info, err := root.Lstat(part)
		if err != nil {
			_ = root.Close()

			return nil, "", false, err
		}

		if err := validateDirectory(info, index == len(parts)-1, systemOwner); err != nil {
			_ = root.Close()

			return nil, "", false, err
		}

		next, err := root.OpenRoot(part)
		_ = root.Close()

		if err != nil {
			return nil, "", false, err
		}

		root = next
	}

	if err := root.Mkdir("search", privateDirectoryMode); err != nil && !errors.Is(err, os.ErrExist) {
		_ = root.Close()

		return nil, "", false, err
	}

	info, err := root.Lstat("search")
	if err == nil {
		err = validateDirectory(info, true, systemOwner)
	}

	if err != nil {
		_ = root.Close()

		return nil, "", false, err
	}

	search, err := root.OpenRoot("search")
	_ = root.Close()

	if err != nil {
		return nil, "", false, err
	}

	fresh, err := prepareDatabase(search)
	if err != nil {
		_ = search.Close()

		return nil, "", false, err
	}

	return search, filepath.Join(abs, "search", "search.sqlite"), fresh, nil
}

func prepareDatabase(root *os.Root) (bool, error) {
	missing := false

	for _, name := range []string{"search.sqlite", "search.sqlite-wal", "search.sqlite-shm", "search.sqlite-journal"} {
		info, err := root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			if name == "search.sqlite" {
				missing = true
			}

			continue
		}

		if err != nil {
			return false, err
		}

		if err := validateFile(info); err != nil {
			return false, err
		}

		if missing {
			return false, fmt.Errorf("%w: auxiliary file without database", ErrUnsafePath)
		}
	}

	if !missing {
		return false, validateHeader(root)
	}

	file, err := root.OpenFile("search.sqlite", os.O_CREATE|os.O_EXCL|os.O_RDWR, privateFileMode)
	if err != nil {
		return false, err
	}

	if err := file.Close(); err != nil {
		return false, err
	}

	return true, nil
}

func validateHeader(root *os.Root) error {
	file, err := root.Open("search.sqlite")
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	header := make([]byte, databaseHeaderBytes)
	if _, err := io.ReadFull(file, header); err != nil {
		return fmt.Errorf("%w: database header", ErrCorrupt)
	}

	if !bytes.Equal(header[:16], []byte("SQLite format 3\x00")) {
		return fmt.Errorf("%w: database header", ErrCorrupt)
	}

	if binary.BigEndian.Uint32(header[68:72]) != applicationID || binary.BigEndian.Uint32(header[60:64]) != schemaVersion {
		return fmt.Errorf("%w: application or schema version", ErrSchema)
	}

	return nil
}

func validateAuxiliary(root *os.Root) error {
	for _, name := range []string{"search.sqlite", "search.sqlite-wal", "search.sqlite-shm", "search.sqlite-journal"} {
		info, err := root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) && name != "search.sqlite" {
			continue
		}

		if err != nil {
			return err
		}

		if err := validateFile(info); err != nil {
			return err
		}
	}

	return nil
}
