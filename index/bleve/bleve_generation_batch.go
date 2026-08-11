package bleve

import (
	"context"
	"fmt"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

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
