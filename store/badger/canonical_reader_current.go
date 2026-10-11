package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// CurrentScopeChangeSeq returns the canonical watermark for one exact scope.
func (r *CanonicalReader) CurrentScopeChangeSeq(ctx context.Context, scope memory.Scope) (uint64, error) {
	if err := canonicalReaderContext(ctx); err != nil {
		return 0, err
	}

	if r == nil || r.store == nil {
		return 0, memory.PermanentError(memory.CodeDisabled, "canonical reader is unavailable", nil)
	}

	state, err := r.store.LoadScopeState(ctx, scope)
	if err != nil {
		return 0, err
	}

	return state.ChangeSeq, nil
}
