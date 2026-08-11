package bleve

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func loadLegacyActiveGeneration(projection *BleveRecallProjection) error {
	marker, err := os.ReadFile(filepath.Join(projection.root, bleveActiveGenerationFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read bleve active generation: %w", err)
	}
	id := strings.TrimSpace(string(marker))
	index, err := openCommittedBleveGeneration(projection.root, id)
	if err != nil {
		return err
	}
	projection.activeID = id
	projection.active = index
	return nil
}

func loadScopedActiveGenerations(projection *BleveRecallProjection) error {
	entries, err := os.ReadDir(projection.root)
	if err != nil {
		return fmt.Errorf("read bleve projection markers: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, bleveScopedActivePrefix) {
			continue
		}
		scopeKey := strings.TrimPrefix(name, bleveScopedActivePrefix)
		if strings.Contains(scopeKey, ".tmp.") {
			continue
		}
		if len(scopeKey) != 64 || !isLowerHex(scopeKey) {
			return memory.PermanentError(memory.CodeStoreFailure, "bleve scoped active marker is invalid", nil)
		}
		marker, err := os.ReadFile(filepath.Join(projection.root, name))
		if err != nil {
			return fmt.Errorf("read bleve scoped active generation: %w", err)
		}
		id := strings.TrimSpace(string(marker))
		index, err := openCommittedBleveGeneration(projection.root, id)
		if err != nil {
			return err
		}
		projection.activeByScope[scopeKey] = index
		projection.activeScopeIDs[scopeKey] = id
	}
	return nil
}
