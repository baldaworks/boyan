package bleve

import (
	"context"
	"fmt"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/blevesearch/bleve/v2"
)

// ScrubCanonicalForget removes projection documents for an already durable
// logical-forget decision.  The active generation is disposable and may be
// updated in place for this bounded maintenance operation; canonical denial
// remains the read gate if the projection is unavailable or stale.
func (p *BleveRecallProjection) ScrubCanonicalForget(
	ctx context.Context,
	scope memory.Scope,
	sourceIDs, revisionIDs []string,
) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}

	if err := validateForgetScrub(scope, sourceIDs, revisionIDs); err != nil {
		return err
	}

	if p == nil {
		return memory.PermanentError(memory.CodeDisabled, "bleve projection is unavailable", nil)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return projectionClosedError()
	}

	index := p.activeByScope[projectionScopeKey(scope)]
	if index == nil {
		index = p.active
	}

	if index == nil {
		return nil
	}

	return scrubCanonicalIndex(ctx, index, scope, sourceIDs, revisionIDs)
}

func scrubCanonicalIndex(
	ctx context.Context,
	index bleve.Index,
	scope memory.Scope,
	sourceIDs,
	revisionIDs []string,
) error {
	searchRequest := buildForgetScrubSearch(scope, sourceIDs, revisionIDs)

	result, err := index.SearchInContext(ctx, searchRequest)
	if err != nil {
		return fmt.Errorf("search bleve forget scrub candidates: %w", err)
	}

	if len(result.Hits) > memory.MaxSnapshotItems {
		return memory.PermanentError(memory.CodeLimitExceeded, "bleve forget scrub candidate bound exceeded", nil)
	}

	if len(result.Hits) == 0 {
		return nil
	}

	batch, err := scrubDeletionBatch(index, scope, result.Hits)
	if err != nil {
		return err
	}

	if err := index.Batch(batch); err != nil {
		return fmt.Errorf("commit bleve forget scrub deletion: %w", err)
	}

	return nil
}
