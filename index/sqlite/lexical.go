package sqlite

import (
	"context"
	"errors"
	"time"

	"github.com/baldaworks/boyan/pkg/memory"
)

// SearchRecall returns bounded lexical candidate identities for canonical hydration.
func (store *Store) SearchRecall(ctx context.Context,
	request memory.RecallRequest) ([]memory.RecallProjectionHit, error) {
	if ctx == nil {
		return nil, errors.New("sqlite search context is nil")
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	effective := time.Now().UTC()
	if request.AsOf != nil {
		effective = request.AsOf.UTC()
	}

	normalized, err := memory.NormalizeRecallRequest(request)
	if err != nil {
		return nil, err
	}

	if store == nil {
		return nil, memory.PermanentError(memory.CodeDisabled, "sqlite projection is unavailable", nil)
	}

	if normalized.MinScopeChangeSeq != 0 {
		request.AsOf = &effective

		return store.SearchRecallConsistent(ctx, request)
	}

	snapshot, err := store.read(ctx)
	if err != nil {
		return nil, err
	}

	defer func() { _ = snapshot.Close() }()

	build, err := snapshot.activeLexicalBuild(normalized.Scope)
	if err != nil {
		return nil, err
	}

	const candidateMultiplier = 4

	depth := min(normalized.Limit*candidateMultiplier, memory.MaxRecallCandidates)

	return snapshot.searchLexical(normalized, effective, build, depth)
}
