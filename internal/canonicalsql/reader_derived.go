package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// SearchDerived retains compatibility filtering and ranking over the bounded
// canonical active page. A fresh authority barrier certifies the hydrated result.
func (o *Owner) SearchDerived(ctx context.Context,
	request memory.DerivedSearchRequest) (memory.DerivedSearchResponse, error) {
	if err := readerContext(ctx, o); err != nil {
		return memory.DerivedSearchResponse{}, err
	}

	normalized, err := memory.NormalizeDerivedSearchRequest(request)
	if err != nil {
		return memory.DerivedSearchResponse{}, err
	}

	var (
		response memory.DerivedSearchResponse
		captured readerSnapshot
	)

	err = o.View(ctx, func(ctx context.Context, tx *Tx) error {
		state, err := readerScopeState(ctx, tx, normalized.Scope)
		if err != nil {
			return err
		}

		if state.ChangeSeq < normalized.MinScopeChangeSeq {
			return memory.RetryableError(memory.CodeConflict, "canonical search watermark has not caught up", nil)
		}

		records, err := readerRecallTail(ctx, tx, derivedRecallRequest(normalized),
			uint32(memory.MaxRecallCandidates), &captured)
		if err != nil {
			return err
		}

		results, err := appendDerivedSearchResults(records, normalized)
		if err != nil {
			return err
		}

		response, err = derivedSearchResponse(normalized, results)
		if err != nil {
			return err
		}

		return ctx.Err()
	})
	if err != nil {
		return memory.DerivedSearchResponse{}, err
	}

	if err := o.finalReaderBarrier(ctx, captured); err != nil {
		return memory.DerivedSearchResponse{}, err
	}

	return response, nil
}
