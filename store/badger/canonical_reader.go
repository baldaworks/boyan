package badger

import (
	"context"
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// CanonicalReader is the bounded application adapter over the canonical
// Badger records. It deliberately implements the storage-neutral recall port
// and keeps Badger keys, envelopes, and payload layout inside state.
type CanonicalReader struct {
	store *CanonicalStore
}

// NewCanonicalReader constructs the canonical read adapter for one Badger
// owner. The reader does not open a second database handle.
func NewCanonicalReader(store *CanonicalStore) (*CanonicalReader, error) {
	if store == nil {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "canonical reader store is required", nil)
	}
	return &CanonicalReader{store: store}, nil
}

var _ memory.RecallCanonicalReader = (*CanonicalReader)(nil)
var _ memory.CanonicalDerivedReader = (*CanonicalReader)(nil)

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

type canonicalRecallView struct {
	item       memory.MemoryItem
	revision   memory.MemoryRevision
	state      memory.RevisionState
	sourceRefs []memory.SourceRef
	sourceIDs  []string
	sessions   []string
	compat     *memory.CanonicalCompatibilityPayload
}

var (
	errCanonicalInactive = errors.New("canonical revision is inactive")
	errCanonicalDenied   = errors.New("canonical revision is denied")
)

func canonicalReaderContext(ctx context.Context) error {
	if ctx == nil {
		return memory.PermanentError(memory.CodeInvalidDerived, "canonical reader context is required", nil)
	}
	select {
	case <-ctx.Done():
		return memory.RetryableError(memory.CodeTimeout, "canonical reader context ended", ctx.Err())
	default:
		return nil
	}
}

func isCanonicalMissing(err error) bool {
	code, _, ok := memory.ClassifyError(err)
	return ok && (code == memory.CodeNotFound || code == memory.CodeForgotten)
}
