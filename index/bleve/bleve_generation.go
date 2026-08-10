package bleve

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// Index adds one validated document to a build generation.
func (g *BleveGeneration) Index(ctx context.Context, document memory.RecallProjectionDocument) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}
	if err := document.Validate(); err != nil {
		return err
	}
	if g == nil {
		return memory.PermanentError(memory.CodeDisabled, "bleve generation is unavailable", nil)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.index == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "bleve generation is closed", nil)
	}
	if g.committed {
		return memory.PermanentError(memory.CodeConflict, "bleve generation was already committed", nil)
	}
	if err := g.batch.Index(document.RevisionID, bleveDocument(document)); err != nil {
		return fmt.Errorf("index bleve recall document: %w", err)
	}
	batchSize := g.batchSize
	if batchSize <= 0 {
		batchSize = bleveBatchSize
	}
	if g.batch.Size() >= batchSize {
		if err := g.index.Batch(g.batch); err != nil {
			return fmt.Errorf("commit bleve recall batch: %w", err)
		}
		g.batch.Reset()
	}
	return nil
}

// Delete removes one revision from a build generation before commit.
func (g *BleveGeneration) Delete(ctx context.Context, revisionID string) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(revisionID) == "" {
		return memory.PermanentError(memory.CodeInvalidDerived, "bleve revision id is required", nil)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.index == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "bleve generation is closed", nil)
	}
	if g.committed {
		return memory.PermanentError(memory.CodeConflict, "bleve generation was already committed", nil)
	}
	g.batch.Delete(revisionID)
	return nil
}

// Commit durably applies the pending generation batch. Activation is a
// separate operation, so a dirty/partial generation is never advertised.
func (g *BleveGeneration) Commit(ctx context.Context) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}
	if g == nil {
		return memory.PermanentError(memory.CodeDisabled, "bleve generation is unavailable", nil)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.index == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "bleve generation is closed", nil)
	}
	if g.committed {
		return nil
	}
	if g.batch.Size() > 0 {
		if err := g.index.Batch(g.batch); err != nil {
			return fmt.Errorf("commit bleve recall generation: %w", err)
		}
		g.batch.Reset()
	}
	if err := os.WriteFile(filepath.Join(g.path, bleveCommittedGenerationFile), []byte(BleveRecallProjectionSchema+"\n"), 0o600); err != nil {
		return fmt.Errorf("write bleve generation commit marker: %w", err)
	}
	g.committed = true
	return nil
}

// Close closes a build generation. It does not activate it.
func (g *BleveGeneration) Close() error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil
	}
	g.closed = true
	if g.index == nil {
		return nil
	}
	err := g.index.Close()
	g.index = nil
	return err
}
