package bleve

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/blevesearch/bleve/v2"
)

func openCommittedBleveGeneration(root, id string) (bleve.Index, error) {
	if !validBleveGenerationID(id) {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "bleve active generation marker is invalid", nil)
	}
	path := filepath.Join(root, bleveGenerationDirectory, id)
	committed, err := os.ReadFile(filepath.Join(path, bleveCommittedGenerationFile))
	if err != nil || strings.TrimSpace(string(committed)) != BleveRecallProjectionSchema {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "bleve active generation is not committed", err)
	}
	index, err := bleve.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open bleve active generation: %w", err)
	}
	return index, nil
}

func isLowerHex(value string) bool {
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
