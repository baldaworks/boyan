package memoryops

import (
	"context"

	projection "github.com/baldaworks/boyan/internal/projection"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Forgetter is the canonical forgetting capability used by runtime and MCP.
type Forgetter interface {
	ForgetSource(ctx context.Context, command memory.ForgetSourceCommand) (memory.ForgetOutcome, error)
	ForgetScope(ctx context.Context, command memory.ForgetScopeCommand) (memory.ForgetOutcome, error)
}

type ProjectionSyncer = projection.Syncer
