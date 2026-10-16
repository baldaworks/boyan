//go:build unix

package sqlite

import (
	"fmt"
	"os"
	"syscall"
)

//nolint:cyclop // Ownership, type, and permission predicates are audited together.
func validateDirectory(info os.FileInfo, private bool, systemOwner uint32) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: directory type", ErrUnsafePath)
	}

	owner := uint32(os.Geteuid())
	if private {
		if stat.Uid != owner || info.Mode().Perm() != 0o700 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
			return fmt.Errorf("%w: private directory", ErrUnsafePath)
		}

		return nil
	}
	// System-owned ancestors are trusted. A system-owned sticky temporary root
	// permits private per-owner children without allowing other users to rename them.
	if stat.Uid != systemOwner && stat.Uid != owner {
		return fmt.Errorf("%w: ancestor owner", ErrUnsafePath)
	}

	if info.Mode().Perm()&0o022 != 0 && !(stat.Uid == systemOwner && info.Mode()&os.ModeSticky != 0) {
		return fmt.Errorf("%w: writable ancestor", ErrUnsafePath)
	}

	return nil
}

// Filesystem-root ownership can be mapped (for example to overflowuid) inside
// a user namespace. Trust the opened system root's owner, never a hardcoded
// fallback UID. Private state still must belong to this process's exact euid.
func filesystemRootOwner(root *os.Root) (uint32, error) {
	info, err := root.Stat(".")
	if err != nil {
		return 0, err
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("%w: root ownership", ErrUnsafePath)
	}

	return stat.Uid, nil
}

func validateFile(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Uid != uint32(os.Geteuid()) ||
		info.Mode().Perm() != privateFileMode || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return fmt.Errorf("%w: private regular file required", ErrUnsafePath)
	}

	if stat.Nlink != 1 {
		return fmt.Errorf("%w: linked database file", ErrUnsafePath)
	}

	return nil
}
