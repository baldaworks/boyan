package bleve

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

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
