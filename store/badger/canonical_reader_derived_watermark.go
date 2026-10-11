package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (r *CanonicalReader) ensureDerivedSearchWatermark(
	ctx context.Context,
	normalized memory.DerivedSearchRequest,
) error {
	if normalized.MinScopeChangeSeq == 0 {
		return nil
	}

	current, err := r.CurrentScopeChangeSeq(ctx, normalized.Scope)
	if err != nil {
		return err
	}

	if current < normalized.MinScopeChangeSeq {
		return memory.RetryableError(memory.CodeConflict, "canonical search watermark has not caught up", nil)
	}

	return nil
}
