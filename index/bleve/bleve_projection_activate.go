package bleve

import (
	"context"
	"path/filepath"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/blevesearch/bleve/v2"
)

// ActivateGeneration opens a committed generation and atomically switches the
// unscoped process-wide active marker. New application wiring should use
// ActivateGenerationForScope so activating one exact scope cannot discard
// another scope's disposable generation.
func (p *BleveRecallProjection) ActivateGeneration(ctx context.Context, id string) error {
	return p.activateGeneration(ctx, memory.Scope{}, id, false)
}

// ActivateGenerationForScope opens a committed generation and switches the
// active marker for one exact scope. Generations remain disposable indexes;
// canonical state is still the source of truth during recall hydration.
func (p *BleveRecallProjection) ActivateGenerationForScope(ctx context.Context, scope memory.Scope, id string) error {
	if err := scope.Validate(); err != nil {
		return err
	}

	return p.activateGeneration(ctx, scope, id, true)
}

func (p *BleveRecallProjection) activateGeneration(
	ctx context.Context,
	scope memory.Scope,
	id string,
	scoped bool,
) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}

	if p == nil || !validBleveGenerationID(id) {
		return memory.PermanentError(memory.CodeInvalidDerived, "bleve activation is invalid", nil)
	}

	p.mu.RLock()

	if p.closed {
		p.mu.RUnlock()

		return projectionClosedError()
	}

	root := p.root
	p.mu.RUnlock()

	index, err := openCommittedBleveGeneration(root, id)
	if err != nil {
		return memory.PermanentError(memory.CodeConflict, "bleve generation is not durably committed", err)
	}

	markerPath := filepath.Join(root, bleveActiveGenerationFile)

	scopeKey := ""
	if scoped {
		scopeKey = projectionScopeKey(scope)
		markerPath = filepath.Join(root, bleveScopedActivePrefix+scopeKey)
	}

	previous, err := p.installActiveGeneration(markerPath, scopeKey, id, index, scoped)
	if err != nil {
		_ = index.Close()

		return err
	}

	if previous != nil {
		return previous.Close()
	}

	return nil
}

func (p *BleveRecallProjection) installActiveGeneration(
	markerPath, scopeKey, id string,
	index bleve.Index,
	scoped bool,
) (bleve.Index, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil, projectionClosedError()
	}

	if err := writeActiveGenerationMarker(markerPath, id); err != nil {
		return nil, err
	}

	return p.swapActiveGenerationLocked(scopeKey, id, index, scoped), nil
}
