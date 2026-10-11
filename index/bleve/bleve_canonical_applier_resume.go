package bleve

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// ResumeProjection reconciles a clean durable checkpoint with its physical index.
func (a *BleveCanonicalApplier) ResumeProjection(ctx context.Context, scope memory.Scope, id string) (bool, error) {
	if err := canonicalContextError(ctx); err != nil {
		return false, err
	}

	p := a.projection
	p.mu.RLock()
	current := p.activeScopeIDs[projectionScopeKey(scope)]
	root := p.root
	p.mu.RUnlock()

	marker, err := os.ReadFile(filepath.Join(root, bleveGenerationDirectory, id, bleveCommittedGenerationFile))
	if err != nil || strings.TrimSpace(string(marker)) != BleveRecallProjectionSchema {
		return false, nil
	}

	if current == id {
		return true, nil
	}

	if err := p.ActivateGenerationForScope(ctx, scope, id); err != nil {
		return false, nil
	}

	return true, nil
}

func (a *BleveCanonicalApplier) borrowGeneration(id string) *BleveGeneration {
	p := a.projection

	p.mu.RLock()
	defer p.mu.RUnlock()

	for scopeKey, activeID := range p.activeScopeIDs {
		if activeID == id {
			index := p.activeByScope[scopeKey]

			return &BleveGeneration{
				id:         id,
				path:       filepath.Join(p.root, bleveGenerationDirectory, id),
				batchSize:  p.batchSize,
				index:      index,
				batch:      index.NewBatch(),
				borrowed:   true,
				continuous: true,
			}
		}
	}

	return nil
}
