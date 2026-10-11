package bleve

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/blevesearch/bleve/v2"
)

// NewBleveRecallProjection opens the active generation under root. The root
// is created if necessary; with no active marker Search safely returns no
// projection candidates so callers can use the bounded canonical fallback.
const projectionDirectoryMode = 0o700

func NewBleveRecallProjection(root string) (*BleveRecallProjection, error) {
	return openWithConfig(root, DefaultConfig())
}

// Open opens a portable rebuildable lexical projection. The directory is
// created if necessary and may be reopened by another process after Close.
func Open(root string, configs ...Config) (*BleveRecallProjection, error) {
	config := DefaultConfig()
	if len(configs) > 0 {
		config = configs[0]
	}

	if len(configs) > 1 {
		return nil, memory.PermanentError(memory.CodeInvalidDerived, "bleve projection accepts at most one config", nil)
	}

	return openWithConfig(root, config)
}

func openWithConfig(root string, config Config) (*BleveRecallProjection, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, memory.PermanentError(memory.CodeInvalidDerived, "bleve projection root is required", nil)
	}

	if config.BatchSize <= 0 {
		config.BatchSize = bleveBatchSize
	}

	if config.BatchSize > memory.MaxSnapshotItems {
		return nil, memory.PermanentError(memory.CodeLimitExceeded, "bleve batch size exceeds the projection bound", nil)
	}

	if err := os.MkdirAll(filepath.Join(root, bleveGenerationDirectory), projectionDirectoryMode); err != nil {
		return nil, fmt.Errorf("create bleve projection root: %w", err)
	}

	projection := &BleveRecallProjection{
		root:           root,
		batchSize:      config.BatchSize,
		activeByScope:  make(map[string]bleve.Index),
		activeScopeIDs: make(map[string]string),
	}
	if err := loadLegacyActiveGeneration(projection); err != nil {
		_ = projection.Close()

		return nil, err
	}

	if err := loadScopedActiveGenerations(projection); err != nil {
		_ = projection.Close()

		return nil, err
	}

	return projection, nil
}
