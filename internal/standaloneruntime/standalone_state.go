package standaloneruntime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type stateLayout struct {
	Root          string
	CanonicalDir  string
	ProjectionDir string
}

const stateDirectoryMode = 0o700

func buildStateLayout(root string) (stateLayout, error) {
	trimmed := strings.TrimSpace(root)
	if trimmed == "" {
		return stateLayout{}, fmt.Errorf("Boyan state directory is required")
	}

	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return stateLayout{}, fmt.Errorf("resolve Boyan state directory: %w", err)
	}

	clean := filepath.Clean(abs)

	return stateLayout{
		Root:          clean,
		CanonicalDir:  filepath.Join(clean, boyanCanonicalDirName),
		ProjectionDir: filepath.Join(clean, boyanProjectionDirName),
	}, nil
}

func ensureStandaloneState(layout stateLayout) error {
	if err := os.MkdirAll(layout.Root, stateDirectoryMode); err != nil {
		return fmt.Errorf("create Boyan state root: %w", err)
	}

	for _, directory := range []string{layout.CanonicalDir, layout.ProjectionDir} {
		if err := os.MkdirAll(directory, stateDirectoryMode); err != nil {
			return fmt.Errorf("create Boyan state directory %s: %w", directory, err)
		}
	}

	return nil
}
