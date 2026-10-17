package standaloneruntime

import (
	"context"

	"github.com/baldaworks/boyan/internal/projection"
	"github.com/baldaworks/boyan/pkg/memory"
)

type canonicalDenialRetirer interface {
	ReconcileCanonicalDenials(ctx context.Context, scope memory.Scope) error
}

// Only forgetting joins historical privacy retirement. Ordinary ingestion keeps
// its existing syncer, and semantic scheduling is not retirement completion.
type forgetProjectionSyncer struct {
	retire canonicalDenialRetirer
	next   projection.Syncer
}

func (s *forgetProjectionSyncer) Sync(ctx context.Context, scope memory.Scope) (result error) {
	if ctx == nil {
		return memory.PermanentError(memory.CodeInvalidDerived, "forget projection context is required", nil)
	}
	defer func() {
		if ctx.Err() != nil {
			result = ctx.Err()
		}
	}()

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := scope.Validate(); err != nil {
		return err
	}

	if s == nil || s.retire == nil || s.next == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "forget projection dependencies are required", nil)
	}

	if err := s.retire.ReconcileCanonicalDenials(ctx, scope); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	return s.next.Sync(ctx, scope)
}
