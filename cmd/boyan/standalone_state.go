package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

type stateLayout struct {
	Root          string
	CanonicalDir  string
	ProjectionDir string
}

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
