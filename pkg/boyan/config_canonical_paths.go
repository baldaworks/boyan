package boyan

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const maxCanonicalSymlinks = 40

// Configuration only resolves/stat's paths; it never creates storage or walks
// existing Badger contents. Missing leaves are resolved through known ancestors.
func canonicalResolvedPath(path string) (string, error) {
	return resolveCanonicalAncestors(path, 0)
}

func resolveCanonicalAncestors(path string, links int) (string, error) {
	if links > maxCanonicalSymlinks {
		return "", ErrInvalidCanonicalConfig
	}

	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}

	if !errors.Is(err, os.ErrNotExist) {
		return "", ErrInvalidCanonicalConfig
	}

	target, err := canonicalSymlinkTarget(path)
	if err != nil {
		return "", err
	}

	if target != "" {
		return resolveCanonicalAncestors(target, links+1)
	}

	parent := filepath.Dir(path)
	if parent == path {
		return "", ErrInvalidCanonicalConfig
	}

	resolved, err = resolveCanonicalAncestors(parent, links)
	if err != nil {
		return "", err
	}

	return filepath.Join(resolved, filepath.Base(path)), nil
}

func canonicalSymlinkTarget(path string) (string, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}

	if err != nil {
		return "", ErrInvalidCanonicalConfig
	}

	if info.Mode()&os.ModeSymlink == 0 {
		return "", nil
	}

	target, err := os.Readlink(path)
	if err != nil {
		return "", ErrInvalidCanonicalConfig
	}

	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}

	return filepath.Clean(target), nil
}

func pathsOverlap(first, second string) bool {
	first = strings.TrimSuffix(first, string(filepath.Separator)) + string(filepath.Separator)
	second = strings.TrimSuffix(second, string(filepath.Separator)) + string(filepath.Separator)

	return strings.HasPrefix(first, second) || strings.HasPrefix(second, first)
}

func validateCanonicalPath(stateDir, path string) error {
	resolved, err := canonicalResolvedPath(path)
	if err != nil {
		return err
	}

	info, err := os.Stat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrInvalidCanonicalConfig
	}

	if info != nil && !info.Mode().IsRegular() {
		return ErrInvalidCanonicalConfig
	}

	if err := validateCanonicalDirectories(stateDir, path, resolved); err != nil {
		return err
	}

	return validateCanonicalKnownFiles(stateDir, info)
}

func validateCanonicalDirectories(stateDir, path, resolved string) error {
	for _, name := range []string{"search", "badger"} {
		reserved := filepath.Join(stateDir, name)

		realReserved, err := canonicalResolvedPath(reserved)
		if err != nil {
			return err
		}

		if pathsOverlap(path, reserved) || pathsOverlap(resolved, realReserved) {
			return ErrInvalidCanonicalConfig
		}
	}

	return nil
}

func validateCanonicalKnownFiles(stateDir string, info os.FileInfo) error {
	if info == nil {
		return nil
	}

	// Check known filesystem authority files directly, including hardlinks. Do
	// not enumerate legacy records or reinterpret any populated old file.
	for _, name := range []string{
		"search/search.sqlite", "search/search.sqlite-wal", "search/search.sqlite-shm",
		"badger/MANIFEST", "badger/KEYREGISTRY", "badger/LOCK", "badger/DISCARD",
	} {
		reserved, err := os.Stat(filepath.Join(stateDir, filepath.FromSlash(name)))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return ErrInvalidCanonicalConfig
		}

		if info != nil && reserved != nil && os.SameFile(info, reserved) {
			return ErrInvalidCanonicalConfig
		}
	}

	return nil
}
