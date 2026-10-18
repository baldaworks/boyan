package canonicalcore

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// TurnStore supplies only the canonical operations consumed by turn processing.
// Optional identity and operation readers remain capabilities of the same store.
type TurnStore interface {
	LoadScopeState(ctx context.Context, scope memory.Scope) (memory.ScopeState, error)
	ApplyCanonicalMutation(ctx context.Context, mutation memory.CanonicalMutation) (memory.CanonicalMutationOutcome, error)
	ScanActiveMemory(ctx context.Context, request memory.ActiveMemoryScanRequest) ([]memory.ActiveCanonicalMemory, error)
}
