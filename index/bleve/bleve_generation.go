package bleve

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/blevesearch/bleve/v2"
)

// NewGeneration starts a new disposable generation. Existing generation IDs
// are rejected to avoid silently mixing partial rebuilds with an old index.
func (p *BleveRecallProjection) NewGeneration(id string) (*BleveGeneration, error) {
	if p == nil {
		return nil, memory.PermanentError(memory.CodeDisabled, "bleve projection is unavailable", nil)
	}

	if !validBleveGenerationID(id) {
		return nil, memory.PermanentError(memory.CodeInvalidDerived, "bleve generation id is invalid", nil)
	}

	p.mu.RLock()
	root := p.root
	p.mu.RUnlock()

	path := filepath.Join(root, bleveGenerationDirectory, id)
	if _, err := os.Stat(path); err == nil {
		return nil, memory.PermanentError(memory.CodeConflict, "bleve generation already exists", nil)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect bleve generation: %w", err)
	}

	index, err := bleve.New(path, bleveRecallMapping())
	if err != nil {
		return nil, fmt.Errorf("create bleve generation: %w", err)
	}

	return &BleveGeneration{id: id, path: path, batchSize: p.batchSize, index: index, batch: index.NewBatch()}, nil
}
